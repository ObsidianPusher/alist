package handles

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/alist-org/alist/v3/internal/driver"
	"github.com/alist-org/alist/v3/internal/model"
	"github.com/alist-org/alist/v3/internal/op"
	"github.com/alist-org/alist/v3/internal/task"

	"github.com/alist-org/alist/v3/internal/fs"
	"github.com/alist-org/alist/v3/pkg/utils"
	"github.com/alist-org/alist/v3/server/common"
	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
	"github.com/xhofe/tache"
)

type baiduDirection uint8

const (
	baiduDownload baiduDirection = iota
	baiduUpload
	defaultTaskListLimit = 100
	maxTaskListLimit     = 500
)

type TaskInfo struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	GroupID     string      `json:"group_id"`
	IsGroup     bool        `json:"is_group,omitempty"`
	Creator     string      `json:"creator"`
	CreatorRole int         `json:"creator_role"`
	State       tache.State `json:"state"`
	Status      string      `json:"status"`
	Progress    float64     `json:"progress"`
	Error       string      `json:"error"`
	DisplayPath string      `json:"display_path,omitempty"`
	TotalBytes  int64       `json:"total_bytes,omitempty"`
	DoneBytes   int64       `json:"done_bytes,omitempty"`
	SubTasks    []TaskInfo  `json:"sub_tasks,omitempty"`
}

type taskUserScope struct {
	isAdmin bool
	uid     uint
}

type cryptAddition struct {
	RemotePath string `json:"remote_path"`
}

func normalizeTaskProgress(progress float64, state tache.State) float64 {
	progress = task.NormalizeProgress(progress)
	if progress >= 100 && state != tache.StateSucceeded {
		// Keep a small headroom for non-terminal states to avoid showing full progress too early.
		return 99.9
	}
	return progress
}

func getTaskInfo[T task.TaskInfoWithCreator](task T) TaskInfo {
	errMsg := ""
	if task.GetErr() != nil {
		errMsg = task.GetErr().Error()
	}
	progress := normalizeTaskProgress(task.GetProgress(), task.GetState())
	creatorName := ""
	creatorRole := -1
	if task.GetCreator() != nil {
		creatorName = task.GetCreator().Username
		creatorRole = task.GetCreator().Role
	}
	return TaskInfo{
		ID:          task.GetID(),
		Name:        task.GetName(),
		GroupID:     task.GetGroupID(),
		Creator:     creatorName,
		CreatorRole: creatorRole,
		State:       task.GetState(),
		Status:      task.GetStatus(),
		Progress:    progress,
		Error:       errMsg,
		DisplayPath: taskDisplayPath(task),
		TotalBytes:  taskTotalBytes(task),
		DoneBytes:   taskDoneBytes(task),
	}
}

func getTaskInfos[T task.TaskInfoWithCreator](tasks []T) []TaskInfo {
	return utils.MustSliceConvert(tasks, getTaskInfo[T])
}

func groupTaskInfos[T task.TaskInfoWithCreator](tasks []T, groupLabel string) []TaskInfo {
	result := make([]TaskInfo, 0, len(tasks))
	groupIndex := map[string]int{}
	groupTasks := map[string][]T{}
	for _, t := range tasks {
		groupID := t.GetGroupID()
		if groupID == "" {
			result = append(result, getTaskInfo(t))
			continue
		}
		groupTasks[groupID] = append(groupTasks[groupID], t)
		if _, ok := groupIndex[groupID]; !ok {
			groupIndex[groupID] = len(result)
			result = append(result, TaskInfo{})
		}
	}
	for groupID, idx := range groupIndex {
		result[idx] = buildGroupTaskInfo(groupID, groupTasks[groupID], groupLabel)
	}
	return result
}

func buildGroupTaskInfo[T task.TaskInfoWithCreator](groupID string, tasks []T, groupLabel string) TaskInfo {
	label := groupLabel
	if label == "" {
		label = "batch task"
	}
	total := len(tasks)
	done := 0
	var (
		progressSum float64
		stateList   []tache.State
		errMsg      string
		creatorName string
		creatorRole int = -1
		displayPath string
		totalBytes  int64
		doneBytes   int64
	)
	for _, t := range tasks {
		stateList = append(stateList, t.GetState())
		progress := normalizeTaskProgress(t.GetProgress(), t.GetState())
		progressSum += progress
		if argsContains(t.GetState(), tache.StateCanceled, tache.StateFailed, tache.StateSucceeded) {
			done++
		}
		if errMsg == "" && t.GetErr() != nil {
			errMsg = t.GetErr().Error()
		}
		if creatorName == "" && t.GetCreator() != nil {
			creatorName = t.GetCreator().Username
			creatorRole = t.GetCreator().Role
		}
		if displayPath == "" {
			displayPath = taskDisplayPath(t)
		}
		totalBytes += taskTotalBytes(t)
		doneBytes += taskDoneBytes(t)
	}
	progress := 0.0
	if total > 0 {
		progress = progressSum / float64(total)
	}
	name := fmt.Sprintf("%s (%d)", label, total)
	status := fmt.Sprintf("%s %d/%d", label, done, total)
	return TaskInfo{
		ID:          groupID,
		Name:        name,
		GroupID:     groupID,
		IsGroup:     true,
		Creator:     creatorName,
		CreatorRole: creatorRole,
		State:       mergeStates(stateList),
		Status:      status,
		Progress:    progress,
		Error:       errMsg,
		DisplayPath: displayPath,
		TotalBytes:  totalBytes,
		DoneBytes:   doneBytes,
		SubTasks:    getTaskInfos(tasks),
	}
}

func taskDisplayPath[T task.TaskInfoWithCreator](tsk T) string {
	switch t := any(tsk).(type) {
	case *fs.CopyTask:
		return t.DstDirPath
	case *localBrowserExtractTask:
		return t.DstDirPath
	default:
		return ""
	}
}

func taskTotalBytes[T task.TaskInfoWithCreator](tsk T) int64 {
	switch t := any(tsk).(type) {
	case *fs.CopyTask:
		if t.TotalBytes > 0 {
			return t.TotalBytes
		}
	}
	return 0
}

func taskDoneBytes[T task.TaskInfoWithCreator](tsk T) int64 {
	switch t := any(tsk).(type) {
	case *fs.CopyTask:
		if t.DoneBytes > 0 {
			return t.DoneBytes
		}
	}
	return 0
}

func mergeStates(states []tache.State) tache.State {
	if len(states) == 0 {
		return tache.StatePending
	}
	has := map[tache.State]bool{}
	for _, s := range states {
		has[s] = true
	}
	if has[tache.StateRunning] {
		return tache.StateRunning
	}
	if has[tache.StatePending] {
		return tache.StatePending
	}
	if has[tache.StateWaitingRetry] {
		return tache.StateWaitingRetry
	}
	if has[tache.StateBeforeRetry] {
		return tache.StateBeforeRetry
	}
	if has[tache.StateCanceling] {
		return tache.StateCanceling
	}
	if has[tache.StateErrored] {
		return tache.StateErrored
	}
	if has[tache.StateFailing] {
		return tache.StateFailing
	}
	if has[tache.StateFailed] {
		return tache.StateFailed
	}
	if has[tache.StateCanceled] {
		return tache.StateCanceled
	}
	return tache.StateSucceeded
}

func filterTaskInfosByStates(infos []TaskInfo, states ...tache.State) []TaskInfo {
	var res []TaskInfo
	for _, info := range infos {
		if argsContains(info.State, states...) {
			res = append(res, info)
		}
	}
	return res
}

func taskInfoSortScore(info TaskInfo) float64 {
	if info.IsGroup {
		score := 0.0
		for _, sub := range info.SubTasks {
			if current := taskInfoSortScore(sub); current > score {
				score = current
			}
		}
		return score
	}
	return info.Progress
}

func sortAndLimitTaskInfos(c *gin.Context, infos []TaskInfo) []TaskInfo {
	sort.SliceStable(infos, func(i, j int) bool {
		si := taskInfoSortScore(infos[i])
		sj := taskInfoSortScore(infos[j])
		if si == sj {
			return infos[i].ID > infos[j].ID
		}
		return si > sj
	})
	limit := defaultTaskListLimit
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			switch {
			case parsed <= 0:
				limit = defaultTaskListLimit
			case parsed > maxTaskListLimit:
				limit = maxTaskListLimit
			default:
				limit = parsed
			}
		}
	}
	if len(infos) > limit {
		return infos[:limit]
	}
	return infos
}

func isTaskTerminalState(state tache.State) bool {
	return argsContains(state, tache.StateCanceled, tache.StateFailed, tache.StateSucceeded)
}

func canTaskBeCanceled(state tache.State) bool {
	return !isTaskTerminalState(state)
}

func canTaskBeRetried(state tache.State) bool {
	return argsContains(state, tache.StateCanceled, tache.StateFailed)
}

func canTaskBeRetriedInGroup(state tache.State) bool {
	return argsContains(state, tache.StateCanceled, tache.StateFailed, tache.StateSucceeded)
}

func resetTaskRuntimeContext[T task.TaskInfoWithCreator](t T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.SetCtx(ctx)
	t.SetCancelFunc(cancel)
}

type beforeRetryableTask interface {
	OnBeforeRetry()
}

func prepareTaskForRetry[T task.TaskInfoWithCreator](t T) {
	if retryable, ok := any(t).(beforeRetryableTask); ok {
		retryable.OnBeforeRetry()
	}
	resetTaskRuntimeContext(t)
}

func buildGroupTaskInfoFromSubTasks(groupID string, subTasks []TaskInfo, groupLabel string) TaskInfo {
	label := groupLabel
	if label == "" {
		label = "batch task"
	}
	total := len(subTasks)
	done := 0
	progressSum := 0.0
	stateList := make([]tache.State, 0, total)
	creatorName := ""
	creatorRole := -1
	errMsg := ""
	displayPath := ""
	totalBytes := int64(0)
	doneBytes := int64(0)
	for _, t := range subTasks {
		stateList = append(stateList, t.State)
		progressSum += normalizeTaskProgress(t.Progress, t.State)
		if isTaskTerminalState(t.State) {
			done++
		}
		if creatorName == "" && t.Creator != "" {
			creatorName = t.Creator
			creatorRole = t.CreatorRole
		}
		if errMsg == "" && t.Error != "" {
			errMsg = t.Error
		}
		if displayPath == "" && t.DisplayPath != "" {
			displayPath = t.DisplayPath
		}
		totalBytes += t.TotalBytes
		doneBytes += t.DoneBytes
	}
	progress := 0.0
	if total > 0 {
		progress = progressSum / float64(total)
	}
	return TaskInfo{
		ID:          groupID,
		Name:        fmt.Sprintf("%s (%d)", label, total),
		GroupID:     groupID,
		IsGroup:     true,
		Creator:     creatorName,
		CreatorRole: creatorRole,
		State:       mergeStates(stateList),
		Status:      fmt.Sprintf("%s %d/%d", label, done, total),
		Progress:    progress,
		Error:       errMsg,
		DisplayPath: displayPath,
		TotalBytes:  totalBytes,
		DoneBytes:   doneBytes,
		SubTasks:    subTasks,
	}
}

func mergeGroupedTaskInfos(infos []TaskInfo, groupLabel string) []TaskInfo {
	if len(infos) <= 1 {
		return infos
	}
	res := make([]TaskInfo, 0, len(infos))
	groupIndex := map[string]int{}
	for _, info := range infos {
		if !info.IsGroup || info.GroupID == "" {
			res = append(res, info)
			continue
		}
		if idx, ok := groupIndex[info.GroupID]; ok {
			existing := res[idx]
			mergedSubs := append(existing.SubTasks, info.SubTasks...)
			if len(mergedSubs) > 1 {
				seen := make(map[string]struct{}, len(mergedSubs))
				deduped := make([]TaskInfo, 0, len(mergedSubs))
				for _, sub := range mergedSubs {
					if sub.ID == "" {
						deduped = append(deduped, sub)
						continue
					}
					if _, exists := seen[sub.ID]; exists {
						continue
					}
					seen[sub.ID] = struct{}{}
					deduped = append(deduped, sub)
				}
				mergedSubs = deduped
			}
			res[idx] = buildGroupTaskInfoFromSubTasks(info.GroupID, mergedSubs, groupLabel)
			continue
		}
		groupIndex[info.GroupID] = len(res)
		res = append(res, info)
	}
	return res
}

func argsContains[T comparable](v T, slice ...T) bool {
	return utils.SliceContains(slice, v)
}

func requireTaskID(c *gin.Context) (string, bool) {
	tid := strings.TrimSpace(c.Query("tid"))
	if tid == "" {
		common.ErrorStrResp(c, "tid required", 400)
		return "", false
	}
	return tid, true
}

func isBaiduStorage(storage driver.Driver) bool {
	return isBaiduStorageWithSeen(storage, map[uint]bool{})
}

func isBaiduStorageWithSeen(storage driver.Driver, seen map[uint]bool) bool {
	if storage == nil || storage.GetStorage() == nil {
		return false
	}
	if ider, ok := storage.(driver.BackendIdentifier); ok {
		if strings.HasPrefix(ider.BackendID(), "baidu:") {
			return true
		}
	}
	st := storage.GetStorage()
	if st.Driver == "BaiduNetdisk" {
		return true
	}
	if st.Driver == "Crypt" {
		if seen[st.ID] {
			return false
		}
		seen[st.ID] = true
		if st.Addition == "" {
			return false
		}
		var add cryptAddition
		if err := json.Unmarshal([]byte(st.Addition), &add); err != nil {
			return false
		}
		if add.RemotePath == "" {
			return false
		}
		remoteStorage, _, err := op.GetStorageAndActualPath(add.RemotePath)
		if err != nil {
			return false
		}
		return isBaiduStorageWithSeen(remoteStorage, seen)
	}
	return false
}

func isBaiduMount(mp string, cache map[string]bool) bool {
	if mp == "" {
		return false
	}
	if val, ok := cache[mp]; ok {
		return val
	}
	storage, err := op.GetStorageByMountPath(mp)
	if err != nil {
		cache[mp] = false
		return false
	}
	val := isBaiduStorage(storage)
	cache[mp] = val
	return val
}

func isCopyTaskBaidu(task *fs.CopyTask, cache map[string]bool) bool {
	return isBaiduMount(task.SrcStorageMp, cache) || isBaiduMount(task.DstStorageMp, cache)
}

func copyTaskMatchesDirection(task *fs.CopyTask, direction baiduDirection, cache map[string]bool) bool {
	srcBaidu := isBaiduMount(task.SrcStorageMp, cache)
	dstBaidu := isBaiduMount(task.DstStorageMp, cache)
	switch direction {
	case baiduDownload:
		return srcBaidu
	case baiduUpload:
		return dstBaidu
	default:
		return false
	}
}

func baiduCopyManagers() []*tache.Manager[*fs.CopyTask] {
	managers := make([]*tache.Manager[*fs.CopyTask], 0, 4)
	if fs.CopyTaskManager != nil {
		managers = append(managers, fs.CopyTaskManager)
	}
	if fs.BaiduDownloadCopyTaskManager != nil {
		managers = append(managers, fs.BaiduDownloadCopyTaskManager)
	}
	if fs.BaiduUploadCopyTaskManager != nil {
		managers = append(managers, fs.BaiduUploadCopyTaskManager)
	}
	if fs.BaiduCopyTaskManager != nil {
		managers = append(managers, fs.BaiduCopyTaskManager)
	}
	return managers
}

func collectBaiduCopyTasks(isAdmin bool, uid uint, direction baiduDirection, cache map[string]bool) []*fs.CopyTask {
	managers := baiduCopyManagers()
	tasks := make([]*fs.CopyTask, 0)
	for _, manager := range managers {
		matched := manager.GetByCondition(func(task *fs.CopyTask) bool {
			return taskOwnedByUser(task, isAdmin, uid) && copyTaskMatchesDirection(task, direction, cache)
		})
		tasks = append(tasks, matched...)
	}
	return tasks
}

func getTaskInfoForManager[T task.TaskInfoWithCreator](manager *tache.Manager[T], tid string, isAdmin bool, uid uint, groupLabel string) (TaskInfo, bool) {
	if strings.TrimSpace(tid) == "" {
		return TaskInfo{}, false
	}
	if manager == nil {
		return TaskInfo{}, false
	}
	if t, ok := manager.GetByID(tid); ok {
		if !taskOwnedByUser(t, isAdmin, uid) {
			return TaskInfo{}, false
		}
		return getTaskInfo(t), true
	}
	groupTasks := manager.GetByCondition(func(task T) bool {
		return task.GetGroupID() == tid && taskOwnedByUser(task, isAdmin, uid)
	})
	if len(groupTasks) == 0 {
		return TaskInfo{}, false
	}
	return buildGroupTaskInfo(tid, groupTasks, groupLabel), true
}

func applyActionForManager[T task.TaskInfoWithCreator](manager *tache.Manager[T], tid string, isAdmin bool, uid uint, action func(task T)) bool {
	if strings.TrimSpace(tid) == "" {
		return false
	}
	if manager == nil {
		return false
	}
	if t, ok := manager.GetByID(tid); ok {
		if !taskOwnedByUser(t, isAdmin, uid) {
			return false
		}
		action(t)
		return true
	}
	groupTasks := manager.GetByCondition(func(task T) bool {
		return task.GetGroupID() == tid && taskOwnedByUser(task, isAdmin, uid)
	})
	if len(groupTasks) == 0 {
		return false
	}
	for _, t := range groupTasks {
		action(t)
	}
	return true
}

func getBaiduCopyTaskInfo(tid string, isAdmin bool, uid uint, direction baiduDirection, cache map[string]bool, groupLabel string) (TaskInfo, bool) {
	if strings.TrimSpace(tid) == "" {
		return TaskInfo{}, false
	}
	for _, manager := range baiduCopyManagers() {
		if t, ok := manager.GetByID(tid); ok {
			if !taskOwnedByUser(t, isAdmin, uid) {
				return TaskInfo{}, false
			}
			if !copyTaskMatchesDirection(t, direction, cache) {
				return TaskInfo{}, false
			}
			return getTaskInfo(t), true
		}
	}
	groupTasks := make([]*fs.CopyTask, 0)
	for _, manager := range baiduCopyManagers() {
		matched := manager.GetByCondition(func(task *fs.CopyTask) bool {
			return task.GetGroupID() == tid && taskOwnedByUser(task, isAdmin, uid) && copyTaskMatchesDirection(task, direction, cache)
		})
		groupTasks = append(groupTasks, matched...)
	}
	if len(groupTasks) == 0 {
		return TaskInfo{}, false
	}
	return buildGroupTaskInfo(tid, groupTasks, groupLabel), true
}

func applyActionForBaiduCopy(tid string, isAdmin bool, uid uint, direction baiduDirection, cache map[string]bool, action func(manager *tache.Manager[*fs.CopyTask], task *fs.CopyTask)) bool {
	if strings.TrimSpace(tid) == "" {
		return false
	}
	for _, manager := range baiduCopyManagers() {
		if t, ok := manager.GetByID(tid); ok {
			if !taskOwnedByUser(t, isAdmin, uid) {
				return false
			}
			if !copyTaskMatchesDirection(t, direction, cache) {
				return false
			}
			action(manager, t)
			return true
		}
	}
	applied := false
	for _, manager := range baiduCopyManagers() {
		groupTasks := manager.GetByCondition(func(task *fs.CopyTask) bool {
			return task.GetGroupID() == tid && taskOwnedByUser(task, isAdmin, uid) && copyTaskMatchesDirection(task, direction, cache)
		})
		if len(groupTasks) == 0 {
			continue
		}
		applied = true
		for _, t := range groupTasks {
			action(manager, t)
		}
	}
	if !applied {
		return false
	}
	return true
}

func getUserInfo(c *gin.Context) (bool, uint, bool) {
	if user, ok := c.Value("user").(*model.User); ok {
		return user.IsAdmin(), user.ID, true
	} else {
		return false, 0, false
	}
}

func requireTaskUserScope(c *gin.Context) (taskUserScope, bool) {
	isAdmin, uid, ok := getUserInfo(c)
	if !ok {
		common.ErrorStrResp(c, "user invalid", 401)
		return taskUserScope{}, false
	}
	return taskUserScope{isAdmin: isAdmin, uid: uid}, true
}

func requireTaskScopeAndID(c *gin.Context) (taskUserScope, string, bool) {
	scope, ok := requireTaskUserScope(c)
	if !ok {
		return taskUserScope{}, "", false
	}
	tid, ok := requireTaskID(c)
	if !ok {
		return taskUserScope{}, "", false
	}
	return scope, tid, true
}

func respondTaskList(c *gin.Context, infos []TaskInfo, states ...tache.State) {
	common.SuccessResp(c, sortAndLimitTaskInfos(c, filterTaskInfosByStates(infos, states...)))
}

func respondMergedTaskInfo(c *gin.Context, groupLabel string, infos ...TaskInfo) bool {
	available := make([]TaskInfo, 0, len(infos))
	for _, info := range infos {
		if info.ID != "" {
			available = append(available, info)
		}
	}
	switch len(available) {
	case 0:
		return false
	case 1:
		common.SuccessResp(c, available[0])
		return true
	default:
		merged := mergeGroupedTaskInfos(available, groupLabel)
		if len(merged) == 0 {
			return false
		}
		common.SuccessResp(c, merged[0])
		return true
	}
}

func applyCombinedTaskAction(c *gin.Context, actions ...func(taskUserScope, string) bool) bool {
	scope, tid, ok := requireTaskScopeAndID(c)
	if !ok {
		return false
	}
	applied := false
	for _, action := range actions {
		if action != nil && action(scope, tid) {
			applied = true
		}
	}
	if !applied {
		common.ErrorStrResp(c, "task not found", 404)
		return false
	}
	common.SuccessResp(c)
	return true
}

func taskOwnedByUser[T task.TaskInfoWithCreator](t T, isAdmin bool, uid uint) bool {
	if isAdmin {
		return true
	}
	creator := t.GetCreator()
	return creator != nil && creator.ID == uid
}

func getTargetedHandler[T task.TaskInfoWithCreator](manager *tache.Manager[T], callback func(c *gin.Context, task T)) gin.HandlerFunc {
	return func(c *gin.Context) {
		scope, ok := requireTaskUserScope(c)
		if !ok {
			return
		}
		t, ok := manager.GetByID(c.Query("tid"))
		if !ok {
			common.ErrorStrResp(c, "task not found", 404)
			return
		}
		if !taskOwnedByUser(t, scope.isAdmin, scope.uid) {
			// to avoid an attacker using error messages to guess valid TID, return a 404 rather than a 403
			common.ErrorStrResp(c, "task not found", 404)
			return
		}
		callback(c, t)
	}
}

func taskRoute[T task.TaskInfoWithCreator](g *gin.RouterGroup, manager *tache.Manager[T]) {
	g.GET("/undone", func(c *gin.Context) {
		scope, ok := requireTaskUserScope(c)
		if !ok {
			return
		}
		common.SuccessResp(c, sortAndLimitTaskInfos(c, getTaskInfos(manager.GetByCondition(func(task T) bool {
			// avoid directly passing the user object into the function to reduce closure size
			return taskOwnedByUser(task, scope.isAdmin, scope.uid) &&
				argsContains(task.GetState(), tache.StatePending, tache.StateRunning, tache.StateCanceling,
					tache.StateErrored, tache.StateFailing, tache.StateWaitingRetry, tache.StateBeforeRetry)
		}))))
	})
	g.GET("/done", func(c *gin.Context) {
		scope, ok := requireTaskUserScope(c)
		if !ok {
			return
		}
		common.SuccessResp(c, sortAndLimitTaskInfos(c, getTaskInfos(manager.GetByCondition(func(task T) bool {
			return taskOwnedByUser(task, scope.isAdmin, scope.uid) &&
				argsContains(task.GetState(), tache.StateCanceled, tache.StateFailed, tache.StateSucceeded)
		}))))
	})
	g.POST("/info", getTargetedHandler(manager, func(c *gin.Context, task T) {
		common.SuccessResp(c, getTaskInfo(task))
	}))
	g.POST("/cancel", getTargetedHandler(manager, func(c *gin.Context, task T) {
		if canTaskBeCanceled(task.GetState()) {
			manager.Cancel(task.GetID())
		}
		common.SuccessResp(c)
	}))
	g.POST("/pause", getTargetedHandler(manager, func(c *gin.Context, task T) {
		// Pause is implemented as cancel; resume uses retry to preserve compatibility.
		if canTaskBeCanceled(task.GetState()) {
			manager.Cancel(task.GetID())
		}
		common.SuccessResp(c)
	}))
	g.POST("/delete", getTargetedHandler(manager, func(c *gin.Context, task T) {
		manager.Remove(task.GetID())
		common.SuccessResp(c)
	}))
	g.POST("/retry", getTargetedHandler(manager, func(c *gin.Context, task T) {
		if canTaskBeRetried(task.GetState()) {
			prepareTaskForRetry(task)
			manager.Retry(task.GetID())
		}
		common.SuccessResp(c)
	}))
	g.POST("/resume", getTargetedHandler(manager, func(c *gin.Context, task T) {
		if canTaskBeRetried(task.GetState()) {
			prepareTaskForRetry(task)
			manager.Retry(task.GetID())
		}
		common.SuccessResp(c)
	}))
	g.POST("/clear_done", func(c *gin.Context) {
		scope, ok := requireTaskUserScope(c)
		if !ok {
			return
		}
		manager.RemoveByCondition(func(task T) bool {
			return taskOwnedByUser(task, scope.isAdmin, scope.uid) &&
				argsContains(task.GetState(), tache.StateCanceled, tache.StateFailed, tache.StateSucceeded)
		})
		common.SuccessResp(c)
	})
	g.POST("/clear_succeeded", func(c *gin.Context) {
		scope, ok := requireTaskUserScope(c)
		if !ok {
			return
		}
		manager.RemoveByCondition(func(task T) bool {
			return taskOwnedByUser(task, scope.isAdmin, scope.uid) && task.GetState() == tache.StateSucceeded
		})
		common.SuccessResp(c)
	})
	g.POST("/retry_failed", func(c *gin.Context) {
		scope, ok := requireTaskUserScope(c)
		if !ok {
			return
		}
		tasks := manager.GetByCondition(func(task T) bool {
			return taskOwnedByUser(task, scope.isAdmin, scope.uid) && task.GetState() == tache.StateFailed
		})
		for _, t := range tasks {
			prepareTaskForRetry(t)
			manager.Retry(t.GetID())
		}
		common.SuccessResp(c)
	})
}

func taskRouteGrouped[T task.TaskInfoWithCreator](g *gin.RouterGroup, manager *tache.Manager[T], groupLabel string, beforeRetry func(T)) {
	filterUserTasks := func(scope taskUserScope) []T {
		return manager.GetByCondition(func(task T) bool {
			return taskOwnedByUser(task, scope.isAdmin, scope.uid)
		})
	}
	g.GET("/undone", func(c *gin.Context) {
		scope, ok := requireTaskUserScope(c)
		if !ok {
			return
		}
		tasks := filterUserTasks(scope)
		grouped := groupTaskInfos(tasks, groupLabel)
		respondTaskList(c, grouped,
			tache.StatePending, tache.StateRunning, tache.StateCanceling, tache.StateErrored,
			tache.StateFailing, tache.StateWaitingRetry, tache.StateBeforeRetry)
	})
	g.GET("/done", func(c *gin.Context) {
		scope, ok := requireTaskUserScope(c)
		if !ok {
			return
		}
		tasks := filterUserTasks(scope)
		grouped := groupTaskInfos(tasks, groupLabel)
		respondTaskList(c, grouped, tache.StateCanceled, tache.StateFailed, tache.StateSucceeded)
	})
	g.POST("/info", func(c *gin.Context) {
		scope, ok := requireTaskUserScope(c)
		if !ok {
			return
		}
		tid, ok := requireTaskID(c)
		if !ok {
			return
		}
		info, found := getTaskInfoForManager(manager, tid, scope.isAdmin, scope.uid, groupLabel)
		if !found {
			common.ErrorStrResp(c, "task not found", 404)
			return
		}
		common.SuccessResp(c, info)
	})
	groupHandler := func(c *gin.Context, action func(task T)) {
		scope, ok := requireTaskUserScope(c)
		if !ok {
			return
		}
		tid, ok := requireTaskID(c)
		if !ok {
			return
		}
		if !applyActionForManager(manager, tid, scope.isAdmin, scope.uid, action) {
			common.ErrorStrResp(c, "task not found", 404)
			return
		}
		common.SuccessResp(c)
	}
	g.POST("/cancel", func(c *gin.Context) {
		groupHandler(c, func(t T) {
			if canTaskBeCanceled(t.GetState()) {
				manager.Cancel(t.GetID())
			}
		})
	})
	g.POST("/pause", func(c *gin.Context) {
		groupHandler(c, func(t T) {
			if canTaskBeCanceled(t.GetState()) {
				manager.Cancel(t.GetID())
			}
		})
	})
	g.POST("/delete", func(c *gin.Context) {
		groupHandler(c, func(t T) {
			manager.Remove(t.GetID())
		})
	})
	g.POST("/retry", func(c *gin.Context) {
		groupHandler(c, func(t T) {
			if canTaskBeRetried(t.GetState()) {
				if beforeRetry != nil {
					beforeRetry(t)
				}
				prepareTaskForRetry(t)
				manager.Retry(t.GetID())
			}
		})
	})
	g.POST("/resume", func(c *gin.Context) {
		groupHandler(c, func(t T) {
			if canTaskBeRetried(t.GetState()) {
				if beforeRetry != nil {
					beforeRetry(t)
				}
				prepareTaskForRetry(t)
				manager.Retry(t.GetID())
			}
		})
	})
	g.POST("/clear_done", func(c *gin.Context) {
		scope, ok := requireTaskUserScope(c)
		if !ok {
			return
		}
		manager.RemoveByCondition(func(task T) bool {
			return taskOwnedByUser(task, scope.isAdmin, scope.uid) &&
				argsContains(task.GetState(), tache.StateCanceled, tache.StateFailed, tache.StateSucceeded)
		})
		common.SuccessResp(c)
	})
	g.POST("/clear_succeeded", func(c *gin.Context) {
		scope, ok := requireTaskUserScope(c)
		if !ok {
			return
		}
		manager.RemoveByCondition(func(task T) bool {
			return taskOwnedByUser(task, scope.isAdmin, scope.uid) && task.GetState() == tache.StateSucceeded
		})
		common.SuccessResp(c)
	})
	g.POST("/retry_failed", func(c *gin.Context) {
		scope, ok := requireTaskUserScope(c)
		if !ok {
			return
		}
		tasks := manager.GetByCondition(func(task T) bool {
			return taskOwnedByUser(task, scope.isAdmin, scope.uid) && task.GetState() == tache.StateFailed
		})
		for _, t := range tasks {
			if beforeRetry != nil {
				beforeRetry(t)
			}
			prepareTaskForRetry(t)
			manager.Retry(t.GetID())
		}
		common.SuccessResp(c)
	})
}

func clearCopyTaskOutput(t *fs.CopyTask) {
	if t == nil || t.DstObjPath == "" || t.DstStorageMp == "" {
		return
	}
	dstStorage, err := op.GetStorageByMountPath(t.DstStorageMp)
	if err != nil {
		log.Warnf("failed to get dst storage for copy task %s cleanup: %v", t.GetID(), err)
		return
	}
	if err := op.Remove(context.Background(), dstStorage, t.DstObjPath); err != nil {
		log.Warnf("failed to cleanup dst path %s for copy task %s: %v", t.DstObjPath, t.GetID(), err)
	}
}

func hasBaiduGroupTarget[T task.TaskInfoWithCreator](tid string, manager *tache.Manager[T], isAdmin bool, uid uint, direction baiduDirection, cache map[string]bool) bool {
	if tid == "" {
		return false
	}
	if manager != nil {
		baseGroup := manager.GetByCondition(func(task T) bool {
			return task.GetGroupID() == tid && taskOwnedByUser(task, isAdmin, uid)
		})
		if len(baseGroup) > 0 {
			return true
		}
	}
	for _, copyManager := range baiduCopyManagers() {
		copyGroup := copyManager.GetByCondition(func(task *fs.CopyTask) bool {
			return task.GetGroupID() == tid && taskOwnedByUser(task, isAdmin, uid) && copyTaskMatchesDirection(task, direction, cache)
		})
		if len(copyGroup) > 0 {
			return true
		}
	}
	return false
}

func taskRouteCopyFiltered(g *gin.RouterGroup, manager *tache.Manager[*fs.CopyTask], groupLabel string) {
	extractManager := localBrowserExtractTaskManager()
	filterCopyTasks := func(scope taskUserScope, cache map[string]bool) []*fs.CopyTask {
		return manager.GetByCondition(func(task *fs.CopyTask) bool {
			return taskOwnedByUser(task, scope.isAdmin, scope.uid) && !isCopyTaskBaidu(task, cache)
		})
	}
	filterExtractTasks := func(scope taskUserScope) []*localBrowserExtractTask {
		return extractManager.GetByCondition(func(task *localBrowserExtractTask) bool {
			return taskOwnedByUser(task, scope.isAdmin, scope.uid)
		})
	}
	getCopyTaskInfo := func(tid string, scope taskUserScope, cache map[string]bool) (TaskInfo, bool) {
		if strings.TrimSpace(tid) == "" {
			return TaskInfo{}, false
		}
		if t, ok := manager.GetByID(tid); ok {
			if !taskOwnedByUser(t, scope.isAdmin, scope.uid) || isCopyTaskBaidu(t, cache) {
				return TaskInfo{}, false
			}
			return getTaskInfo(t), true
		}
		groupTasks := manager.GetByCondition(func(task *fs.CopyTask) bool {
			return task.GetGroupID() == tid && taskOwnedByUser(task, scope.isAdmin, scope.uid) && !isCopyTaskBaidu(task, cache)
		})
		if len(groupTasks) == 0 {
			return TaskInfo{}, false
		}
		return buildGroupTaskInfo(tid, groupTasks, groupLabel), true
	}
	applyCopyAction := func(tid string, scope taskUserScope, cache map[string]bool, action func(task *fs.CopyTask)) bool {
		if strings.TrimSpace(tid) == "" {
			return false
		}
		if t, ok := manager.GetByID(tid); ok {
			if !taskOwnedByUser(t, scope.isAdmin, scope.uid) || isCopyTaskBaidu(t, cache) {
				return false
			}
			action(t)
			return true
		}
		groupTasks := manager.GetByCondition(func(task *fs.CopyTask) bool {
			return task.GetGroupID() == tid && taskOwnedByUser(task, scope.isAdmin, scope.uid) && !isCopyTaskBaidu(task, cache)
		})
		if len(groupTasks) == 0 {
			return false
		}
		for _, t := range groupTasks {
			action(t)
		}
		return true
	}
	g.GET("/undone", func(c *gin.Context) {
		scope, ok := requireTaskUserScope(c)
		if !ok {
			return
		}
		cache := map[string]bool{}
		copyInfos := groupTaskInfos(filterCopyTasks(scope, cache), groupLabel)
		extractInfos := groupTaskInfos(filterExtractTasks(scope), groupLabel)
		merged := mergeGroupedTaskInfos(append(copyInfos, extractInfos...), groupLabel)
		respondTaskList(c, merged,
			tache.StatePending, tache.StateRunning, tache.StateCanceling, tache.StateErrored,
			tache.StateFailing, tache.StateWaitingRetry, tache.StateBeforeRetry)
	})
	g.GET("/done", func(c *gin.Context) {
		scope, ok := requireTaskUserScope(c)
		if !ok {
			return
		}
		cache := map[string]bool{}
		copyInfos := groupTaskInfos(filterCopyTasks(scope, cache), groupLabel)
		extractInfos := groupTaskInfos(filterExtractTasks(scope), groupLabel)
		merged := mergeGroupedTaskInfos(append(copyInfos, extractInfos...), groupLabel)
		respondTaskList(c, merged, tache.StateCanceled, tache.StateFailed, tache.StateSucceeded)
	})
	g.POST("/info", func(c *gin.Context) {
		scope, tid, ok := requireTaskScopeAndID(c)
		if !ok {
			return
		}
		cache := map[string]bool{}
		copyInfo, _ := getCopyTaskInfo(tid, scope, cache)
		extractInfo, _ := getTaskInfoForManager(extractManager, tid, scope.isAdmin, scope.uid, groupLabel)
		if respondMergedTaskInfo(c, groupLabel, copyInfo, extractInfo) {
			return
		}
		common.ErrorStrResp(c, "task not found", 404)
	})
	g.POST("/cancel", func(c *gin.Context) {
		applyCombinedTaskAction(c,
			func(scope taskUserScope, tid string) bool {
				cache := map[string]bool{}
				return applyCopyAction(tid, scope, cache, func(t *fs.CopyTask) {
					if canTaskBeCanceled(t.GetState()) {
						manager.Cancel(t.GetID())
					}
				})
			},
			func(scope taskUserScope, tid string) bool {
				return applyActionForManager(extractManager, tid, scope.isAdmin, scope.uid, func(t *localBrowserExtractTask) {
					if canTaskBeCanceled(t.GetState()) {
						extractManager.Cancel(t.GetID())
					}
				})
			},
		)
	})
	g.POST("/pause", func(c *gin.Context) {
		applyCombinedTaskAction(c,
			func(scope taskUserScope, tid string) bool {
				cache := map[string]bool{}
				return applyCopyAction(tid, scope, cache, func(t *fs.CopyTask) {
					if canTaskBeCanceled(t.GetState()) {
						manager.Cancel(t.GetID())
					}
				})
			},
			func(scope taskUserScope, tid string) bool {
				return applyActionForManager(extractManager, tid, scope.isAdmin, scope.uid, func(t *localBrowserExtractTask) {
					if canTaskBeCanceled(t.GetState()) {
						extractManager.Cancel(t.GetID())
					}
				})
			},
		)
	})
	g.POST("/delete", func(c *gin.Context) {
		applyCombinedTaskAction(c,
			func(scope taskUserScope, tid string) bool {
				cache := map[string]bool{}
				return applyCopyAction(tid, scope, cache, func(t *fs.CopyTask) {
					manager.Remove(t.GetID())
				})
			},
			func(scope taskUserScope, tid string) bool {
				return applyActionForManager(extractManager, tid, scope.isAdmin, scope.uid, func(t *localBrowserExtractTask) {
					extractManager.Remove(t.GetID())
				})
			},
		)
	})
	g.POST("/retry", func(c *gin.Context) {
		applyCombinedTaskAction(c,
			func(scope taskUserScope, tid string) bool {
				cache := map[string]bool{}
				return applyCopyAction(tid, scope, cache, func(t *fs.CopyTask) {
					if canTaskBeRetried(t.GetState()) {
						prepareTaskForRetry(t)
						manager.Retry(t.GetID())
					}
				})
			},
			func(scope taskUserScope, tid string) bool {
				return applyActionForManager(extractManager, tid, scope.isAdmin, scope.uid, func(t *localBrowserExtractTask) {
					if canTaskBeRetried(t.GetState()) {
						prepareTaskForRetry(t)
						extractManager.Retry(t.GetID())
					}
				})
			},
		)
	})
	g.POST("/resume", func(c *gin.Context) {
		applyCombinedTaskAction(c,
			func(scope taskUserScope, tid string) bool {
				cache := map[string]bool{}
				return applyCopyAction(tid, scope, cache, func(t *fs.CopyTask) {
					if canTaskBeRetried(t.GetState()) {
						prepareTaskForRetry(t)
						manager.Retry(t.GetID())
					}
				})
			},
			func(scope taskUserScope, tid string) bool {
				return applyActionForManager(extractManager, tid, scope.isAdmin, scope.uid, func(t *localBrowserExtractTask) {
					if canTaskBeRetried(t.GetState()) {
						prepareTaskForRetry(t)
						extractManager.Retry(t.GetID())
					}
				})
			},
		)
	})
	g.POST("/clear_done", func(c *gin.Context) {
		scope, ok := requireTaskUserScope(c)
		if !ok {
			return
		}
		cache := map[string]bool{}
		manager.RemoveByCondition(func(task *fs.CopyTask) bool {
			return taskOwnedByUser(task, scope.isAdmin, scope.uid) &&
				!isCopyTaskBaidu(task, cache) &&
				argsContains(task.GetState(), tache.StateCanceled, tache.StateFailed, tache.StateSucceeded)
		})
		extractManager.RemoveByCondition(func(task *localBrowserExtractTask) bool {
			return taskOwnedByUser(task, scope.isAdmin, scope.uid) &&
				argsContains(task.GetState(), tache.StateCanceled, tache.StateFailed, tache.StateSucceeded)
		})
		common.SuccessResp(c)
	})
	g.POST("/clear_succeeded", func(c *gin.Context) {
		scope, ok := requireTaskUserScope(c)
		if !ok {
			return
		}
		cache := map[string]bool{}
		manager.RemoveByCondition(func(task *fs.CopyTask) bool {
			return taskOwnedByUser(task, scope.isAdmin, scope.uid) && !isCopyTaskBaidu(task, cache) && task.GetState() == tache.StateSucceeded
		})
		extractManager.RemoveByCondition(func(task *localBrowserExtractTask) bool {
			return taskOwnedByUser(task, scope.isAdmin, scope.uid) && task.GetState() == tache.StateSucceeded
		})
		common.SuccessResp(c)
	})
	g.POST("/retry_failed", func(c *gin.Context) {
		scope, ok := requireTaskUserScope(c)
		if !ok {
			return
		}
		cache := map[string]bool{}
		tasks := manager.GetByCondition(func(task *fs.CopyTask) bool {
			return taskOwnedByUser(task, scope.isAdmin, scope.uid) && !isCopyTaskBaidu(task, cache) && task.GetState() == tache.StateFailed
		})
		for _, t := range tasks {
			prepareTaskForRetry(t)
			manager.Retry(t.GetID())
		}
		extractTasks := extractManager.GetByCondition(func(task *localBrowserExtractTask) bool {
			return taskOwnedByUser(task, scope.isAdmin, scope.uid) && task.GetState() == tache.StateFailed
		})
		for _, t := range extractTasks {
			prepareTaskForRetry(t)
			extractManager.Retry(t.GetID())
		}
		common.SuccessResp(c)
	})
}

func taskRouteBaiduCombined[T task.TaskInfoWithCreator](g *gin.RouterGroup, manager *tache.Manager[T], direction baiduDirection, groupLabel string) {
	filterBaseTasks := func(scope taskUserScope) []T {
		if manager == nil {
			return nil
		}
		return manager.GetByCondition(func(task T) bool {
			return taskOwnedByUser(task, scope.isAdmin, scope.uid)
		})
	}

	getMergedBaiduTaskInfo := func(tid string, scope taskUserScope, cache map[string]bool) (TaskInfo, bool) {
		baseInfo, _ := getTaskInfoForManager(manager, tid, scope.isAdmin, scope.uid, groupLabel)
		copyInfo, _ := getBaiduCopyTaskInfo(tid, scope.isAdmin, scope.uid, direction, cache, groupLabel)
		available := make([]TaskInfo, 0, 2)
		if baseInfo.ID != "" {
			available = append(available, baseInfo)
		}
		if copyInfo.ID != "" {
			available = append(available, copyInfo)
		}
		if len(available) == 0 {
			return TaskInfo{}, false
		}
		if len(available) == 1 {
			return available[0], true
		}
		merged := mergeGroupedTaskInfos(available, groupLabel)
		if len(merged) == 0 {
			return TaskInfo{}, false
		}
		return merged[0], true
	}

	applyBaiduCombinedAction := func(scope taskUserScope, tid string, action func(baseTask T), copyAction func(copyManager *tache.Manager[*fs.CopyTask], copyTask *fs.CopyTask), cache map[string]bool) bool {
		applied := false
		if applyActionForManager(manager, tid, scope.isAdmin, scope.uid, action) {
			applied = true
		}
		if applyActionForBaiduCopy(tid, scope.isAdmin, scope.uid, direction, cache, copyAction) {
			applied = true
		}
		return applied
	}
	g.GET("/undone", func(c *gin.Context) {
		scope, ok := requireTaskUserScope(c)
		if !ok {
			return
		}
		cache := map[string]bool{}
		baseInfos := groupTaskInfos(filterBaseTasks(scope), groupLabel)
		copyInfos := groupTaskInfos(collectBaiduCopyTasks(scope.isAdmin, scope.uid, direction, cache), groupLabel)
		merged := mergeGroupedTaskInfos(append(baseInfos, copyInfos...), groupLabel)
		respondTaskList(c, merged,
			tache.StatePending, tache.StateRunning, tache.StateCanceling, tache.StateErrored,
			tache.StateFailing, tache.StateWaitingRetry, tache.StateBeforeRetry)
	})
	g.GET("/done", func(c *gin.Context) {
		scope, ok := requireTaskUserScope(c)
		if !ok {
			return
		}
		cache := map[string]bool{}
		baseInfos := groupTaskInfos(filterBaseTasks(scope), groupLabel)
		copyInfos := groupTaskInfos(collectBaiduCopyTasks(scope.isAdmin, scope.uid, direction, cache), groupLabel)
		merged := mergeGroupedTaskInfos(append(baseInfos, copyInfos...), groupLabel)
		respondTaskList(c, merged, tache.StateCanceled, tache.StateFailed, tache.StateSucceeded)
	})
	g.POST("/info", func(c *gin.Context) {
		scope, tid, ok := requireTaskScopeAndID(c)
		if !ok {
			return
		}
		cache := map[string]bool{}
		info, found := getMergedBaiduTaskInfo(tid, scope, cache)
		if found {
			common.SuccessResp(c, info)
			return
		}
		common.ErrorStrResp(c, "task not found", 404)
	})
	g.POST("/cancel", func(c *gin.Context) {
		applyCombinedTaskAction(c, func(scope taskUserScope, tid string) bool {
			cache := map[string]bool{}
			return applyBaiduCombinedAction(scope, tid,
				func(t T) {
					if canTaskBeCanceled(t.GetState()) {
						manager.Cancel(t.GetID())
					}
				},
				func(copyManager *tache.Manager[*fs.CopyTask], t *fs.CopyTask) {
					if canTaskBeCanceled(t.GetState()) {
						copyManager.Cancel(t.GetID())
					}
				},
				cache,
			)
		})
	})
	g.POST("/pause", func(c *gin.Context) {
		applyCombinedTaskAction(c, func(scope taskUserScope, tid string) bool {
			cache := map[string]bool{}
			return applyBaiduCombinedAction(scope, tid,
				func(t T) {
					if canTaskBeCanceled(t.GetState()) {
						manager.Cancel(t.GetID())
					}
				},
				func(copyManager *tache.Manager[*fs.CopyTask], t *fs.CopyTask) {
					if canTaskBeCanceled(t.GetState()) {
						copyManager.Cancel(t.GetID())
					}
				},
				cache,
			)
		})
	})
	g.POST("/delete", func(c *gin.Context) {
		applyCombinedTaskAction(c, func(scope taskUserScope, tid string) bool {
			cache := map[string]bool{}
			return applyBaiduCombinedAction(scope, tid,
				func(t T) { manager.Remove(t.GetID()) },
				func(copyManager *tache.Manager[*fs.CopyTask], t *fs.CopyTask) { copyManager.Remove(t.GetID()) },
				cache,
			)
		})
	})
	g.POST("/retry", func(c *gin.Context) {
		isAdmin, uid, ok := getUserInfo(c)
		if !ok {
			common.ErrorStrResp(c, "user invalid", 401)
			return
		}
		tid, ok := requireTaskID(c)
		if !ok {
			return
		}
		cache := map[string]bool{}
		groupTarget := hasBaiduGroupTarget(tid, manager, isAdmin, uid, direction, cache)
		applied := false
		if applyActionForManager(manager, tid, isAdmin, uid, func(t T) {
			canRetry := canTaskBeRetried(t.GetState())
			if groupTarget {
				canRetry = canTaskBeRetriedInGroup(t.GetState())
			}
			if canRetry {
				prepareTaskForRetry(t)
				manager.Retry(t.GetID())
			}
		}) {
			applied = true
		}
		if applyActionForBaiduCopy(tid, isAdmin, uid, direction, cache, func(copyManager *tache.Manager[*fs.CopyTask], t *fs.CopyTask) {
			canRetry := canTaskBeRetried(t.GetState())
			if groupTarget {
				canRetry = canTaskBeRetriedInGroup(t.GetState())
			}
			if canRetry {
				clearCopyTaskOutput(t)
				prepareTaskForRetry(t)
				copyManager.Retry(t.GetID())
			}
		}) {
			applied = true
		}
		if applied {
			common.SuccessResp(c)
			return
		}
		common.ErrorStrResp(c, "task not found", 404)
	})
	g.POST("/resume", func(c *gin.Context) {
		isAdmin, uid, ok := getUserInfo(c)
		if !ok {
			common.ErrorStrResp(c, "user invalid", 401)
			return
		}
		tid, ok := requireTaskID(c)
		if !ok {
			return
		}
		cache := map[string]bool{}
		groupTarget := hasBaiduGroupTarget(tid, manager, isAdmin, uid, direction, cache)
		applied := false
		if applyActionForManager(manager, tid, isAdmin, uid, func(t T) {
			canRetry := canTaskBeRetried(t.GetState())
			if groupTarget {
				canRetry = canTaskBeRetriedInGroup(t.GetState())
			}
			if canRetry {
				prepareTaskForRetry(t)
				manager.Retry(t.GetID())
			}
		}) {
			applied = true
		}
		if applyActionForBaiduCopy(tid, isAdmin, uid, direction, cache, func(copyManager *tache.Manager[*fs.CopyTask], t *fs.CopyTask) {
			canRetry := canTaskBeRetried(t.GetState())
			if groupTarget {
				canRetry = canTaskBeRetriedInGroup(t.GetState())
			}
			if canRetry {
				clearCopyTaskOutput(t)
				prepareTaskForRetry(t)
				copyManager.Retry(t.GetID())
			}
		}) {
			applied = true
		}
		if applied {
			common.SuccessResp(c)
			return
		}
		common.ErrorStrResp(c, "task not found", 404)
	})
	g.POST("/clear_done", func(c *gin.Context) {
		isAdmin, uid, ok := getUserInfo(c)
		if !ok {
			common.ErrorStrResp(c, "user invalid", 401)
			return
		}
		if manager != nil {
			manager.RemoveByCondition(func(task T) bool {
				return taskOwnedByUser(task, isAdmin, uid) &&
					argsContains(task.GetState(), tache.StateCanceled, tache.StateFailed, tache.StateSucceeded)
			})
		}
		cache := map[string]bool{}
		for _, copyManager := range baiduCopyManagers() {
			copyManager.RemoveByCondition(func(task *fs.CopyTask) bool {
				return taskOwnedByUser(task, isAdmin, uid) &&
					copyTaskMatchesDirection(task, direction, cache) &&
					argsContains(task.GetState(), tache.StateCanceled, tache.StateFailed, tache.StateSucceeded)
			})
		}
		common.SuccessResp(c)
	})
	g.POST("/clear_succeeded", func(c *gin.Context) {
		isAdmin, uid, ok := getUserInfo(c)
		if !ok {
			common.ErrorStrResp(c, "user invalid", 401)
			return
		}
		if manager != nil {
			manager.RemoveByCondition(func(task T) bool {
				return taskOwnedByUser(task, isAdmin, uid) && task.GetState() == tache.StateSucceeded
			})
		}
		cache := map[string]bool{}
		for _, copyManager := range baiduCopyManagers() {
			copyManager.RemoveByCondition(func(task *fs.CopyTask) bool {
				return taskOwnedByUser(task, isAdmin, uid) &&
					copyTaskMatchesDirection(task, direction, cache) &&
					task.GetState() == tache.StateSucceeded
			})
		}
		common.SuccessResp(c)
	})
	g.POST("/retry_failed", func(c *gin.Context) {
		isAdmin, uid, ok := getUserInfo(c)
		if !ok {
			common.ErrorStrResp(c, "user invalid", 401)
			return
		}
		if manager != nil {
			tasks := manager.GetByCondition(func(task T) bool {
				return taskOwnedByUser(task, isAdmin, uid) && task.GetState() == tache.StateFailed
			})
			for _, t := range tasks {
				prepareTaskForRetry(t)
				manager.Retry(t.GetID())
			}
		}
		cache := map[string]bool{}
		for _, copyManager := range baiduCopyManagers() {
			copyTasks := copyManager.GetByCondition(func(task *fs.CopyTask) bool {
				return taskOwnedByUser(task, isAdmin, uid) &&
					copyTaskMatchesDirection(task, direction, cache) &&
					task.GetState() == tache.StateFailed
			})
			for _, t := range copyTasks {
				prepareTaskForRetry(t)
				copyManager.Retry(t.GetID())
			}
		}
		common.SuccessResp(c)
	})
}

func SetupTaskRoute(g *gin.RouterGroup) {
	taskRouteGrouped(g.Group("/upload"), fs.UploadTaskManager, "upload batch", nil)
	taskRouteCopyFiltered(g.Group("/copy"), fs.CopyTaskManager, "file operation batch")
	taskRouteBaiduCombined(g.Group("/baidu_upload"), fs.BaiduUploadTaskManager, baiduUpload, "baidu upload batch")
	taskRouteBaiduCombined[*fs.CopyTask](g.Group("/baidu_download"), nil, baiduDownload, "baidu download batch")
}

package task

import (
	"github.com/alist-org/alist/v3/internal/model"
	"github.com/google/uuid"
	"github.com/xhofe/tache"
)

const DefaultProgressPersistStep = 1.0

type TaskWithCreator struct {
	tache.Base
	Creator *model.User
	GroupID string
}

const GroupPrefix = "group-"

func NewGroupID() string {
	return GroupPrefix + uuid.NewString()
}

func (t *TaskWithCreator) SetCreator(creator *model.User) {
	t.Creator = creator
	t.Persist()
}

func (t *TaskWithCreator) GetCreator() *model.User {
	return t.Creator
}

func (t *TaskWithCreator) SetGroupID(id string) {
	t.GroupID = id
	t.Persist()
}

func (t *TaskWithCreator) GetGroupID() string {
	return t.GroupID
}

type TaskInfoWithCreator interface {
	tache.TaskWithInfo
	SetCreator(creator *model.User)
	GetCreator() *model.User
	SetGroupID(id string)
	GetGroupID() string
}

func PersistOnTimestampChange(last *int64, next int64, persist func()) bool {
	if last == nil || persist == nil {
		return false
	}
	if *last == next {
		return false
	}
	*last = next
	persist()
	return true
}

func SetProgressIfChanged(base *tache.Base, next float64, minDelta float64) bool {
	if base == nil {
		return false
	}
	next = NormalizeProgress(next)
	current := NormalizeProgress(base.GetProgress())
	if next == current {
		return false
	}
	if minDelta > 0 && next > 0 && next < 100 && current > 0 && current < 100 {
		delta := next - current
		if delta < 0 {
			delta = -delta
		}
		if delta < minDelta {
			return false
		}
	}
	base.SetProgress(next)
	return true
}

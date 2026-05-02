package baidu_netdisk

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	stdpath "path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Xhofe/go-cache"
	"github.com/alist-org/alist/v3/drivers/base"
	"github.com/alist-org/alist/v3/internal/driver"
	"github.com/alist-org/alist/v3/internal/errs"
	"github.com/alist-org/alist/v3/internal/model"
	"github.com/alist-org/alist/v3/internal/task"
	"github.com/alist-org/alist/v3/pkg/errgroup"
	"github.com/alist-org/alist/v3/pkg/utils"
	"github.com/avast/retry-go"
	log "github.com/sirupsen/logrus"
)

const maxCachedUploadPendingParts = 200000

type baiduUploadProgressState struct {
	Uploadid       string   `json:"uploadid"`
	PendingPartSeq []uint32 `json:"pending_part_seq"`
}

type BaiduNetdisk struct {
	model.Storage
	Addition

	uploadThread            int
	vipType                 int // 会员类型，0普通用户(4G/4M)、1普通会员(10G/16M)、2超级会员(20G/32M)
	downloadPartConcurrency int
	downloadPartSize        int

	listLimiter      *tokenLimiter
	downloadLimiter  *tokenLimiter
	uploadLimiter    *tokenLimiter
	proxyCache       *baiduProxyRangeCache
	dlinkURLCache    cache.ICache[string]
	keepAliveMu      sync.Mutex
	keepAliveCancel  context.CancelFunc
	proxyCacheInitMu sync.Mutex
}

type fdFile interface {
	Fd() uintptr
}

type baiduDownloadRuntimeConfig struct {
	downloadTaskConcurrency int
	partConcurrency         int
	partSize                int
	proxyCacheBytes         int64
}

func dropTempFileCacheRange(f model.File, offset, length int64) {
	if f == nil || length <= 0 {
		return
	}
	ff, ok := f.(fdFile)
	if !ok {
		return
	}
	if err := utils.DropFileCacheByFD(ff.Fd(), offset, length); err != nil {
		log.Debugf("[baidu_netdisk] drop temp file cache failed: offset=%d length=%d err=%v", offset, length, err)
	}
}

func (d *BaiduNetdisk) BackendID() string {
	if d.RefreshToken != "" {
		return "baidu:" + d.RefreshToken + ":" + d.ClientID
	}
	return fmt.Sprintf("baidu:storage:%d", d.Storage.ID)
}

func (d *BaiduNetdisk) BackendPath(path string) string {
	root := utils.FixAndCleanPath(d.RootFolderPath)
	if root == "" || root == "/" {
		return utils.FixAndCleanPath(path)
	}
	return utils.FixAndCleanPath(stdpath.Join(root, path))
}

func (d *BaiduNetdisk) Config() driver.Config {
	return config
}

func (d *BaiduNetdisk) GetAddition() driver.Additional {
	return &d.Addition
}

func (d *BaiduNetdisk) Init(ctx context.Context) error {
	d.stopTokenKeepAlive()
	d.closeProxyCache()
	d.uploadThread = 1
	listTaskConcurrency := d.ListTaskConcurrency
	if listTaskConcurrency <= 0 {
		listTaskConcurrency = defaultListTaskConcurrency
	}
	listTaskConcurrency = clampInt(listTaskConcurrency, 1, 32)
	uploadTaskConcurrency := d.UploadTaskConcurrency
	if uploadTaskConcurrency <= 0 {
		uploadTaskConcurrency = defaultUploadTaskConcurrency
	}
	uploadTaskConcurrency = clampInt(uploadTaskConcurrency, 1, 8)
	downloadTaskConcurrency := d.DownloadTaskConcurrency
	if downloadTaskConcurrency <= 0 {
		downloadTaskConcurrency = defaultDownloadTaskConcurrency
	}
	downloadTaskConcurrency = clampInt(downloadTaskConcurrency, 1, 16)
	d.downloadPartConcurrency = defaultDownloadPartConcurrency
	downloadPartSizeMB := d.DownloadPartSizeMB
	if downloadPartSizeMB <= 0 {
		downloadPartSizeMB = defaultDownloadPartSizeMB
	}
	d.ListTaskConcurrency = listTaskConcurrency
	d.UploadTaskConcurrency = uploadTaskConcurrency
	d.DownloadTaskConcurrency = downloadTaskConcurrency
	d.DownloadPartSizeMB = downloadPartSizeMB
	d.downloadPartSize = megabytesToBytes(downloadPartSizeMB, defaultDownloadPartSizeMB)
	tokenKeepAliveIntervalMin := d.TokenKeepaliveIntervalMin
	if tokenKeepAliveIntervalMin <= 0 {
		tokenKeepAliveIntervalMin = 30
	}
	tokenKeepAliveIntervalMin = clampInt(tokenKeepAliveIntervalMin, 5, 1440)
	d.TokenKeepaliveIntervalMin = tokenKeepAliveIntervalMin

	d.listLimiter = newTokenLimiter(listTaskConcurrency)
	d.downloadLimiter = newTokenLimiter(downloadTaskConcurrency)
	d.uploadLimiter = newTokenLimiter(uploadTaskConcurrency)
	d.dlinkURLCache = cache.NewMemCache[string](cache.WithShards[string](64))
	d.proxyCache = nil

	if _, err := url.Parse(d.UploadAPI); d.UploadAPI == "" || err != nil {
		d.UploadAPI = "https://d.pcs.baidu.com"
	}

	if d.RefreshToken == "" {
		if strings.TrimSpace(d.AuthCode) == "" {
			return errors.New("refresh_token or auth_code is required")
		}
		if err := d.exchangeAuthCode(ctx); err != nil {
			return err
		}
	}
	if d.AccessToken == "" {
		if err := d.refreshToken(ctx); err != nil {
			return err
		}
	}

	res, err := d.get(ctx, "/xpan/nas", map[string]string{
		"method": "uinfo",
	}, nil)
	log.Debugf("[baidu] get uinfo: %s", string(res))
	if err != nil {
		return err
	}
	d.vipType = utils.Json.Get(res, "vip_type").ToInt()
	d.startTokenKeepAlive()
	return nil
}

func (d *BaiduNetdisk) Drop(ctx context.Context) error {
	d.stopTokenKeepAlive()
	d.closeProxyCache()
	return nil
}

func (d *BaiduNetdisk) closeProxyCache() {
	if d.proxyCache == nil {
		return
	}
	unregisterActiveBaiduProxyCache(d.proxyCache)
	d.proxyCache.Close()
	d.proxyCache = nil
}

func (d *BaiduNetdisk) startTokenKeepAlive() {
	interval := time.Duration(d.TokenKeepaliveIntervalMin) * time.Minute
	if interval <= 0 {
		interval = 30 * time.Minute
	}
	ctx, cancel := context.WithCancel(context.Background())
	d.keepAliveMu.Lock()
	if d.keepAliveCancel != nil {
		d.keepAliveCancel()
	}
	d.keepAliveCancel = cancel
	d.keepAliveMu.Unlock()
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := d.keepAliveByRootList(ctx); err != nil && !errors.Is(err, context.Canceled) {
					log.Warnf("[baidu_netdisk] token keepalive list request failed: %v", err)
				}
			}
		}
	}()
}

func (d *BaiduNetdisk) stopTokenKeepAlive() {
	d.keepAliveMu.Lock()
	cancel := d.keepAliveCancel
	d.keepAliveCancel = nil
	d.keepAliveMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (d *BaiduNetdisk) keepAliveByRootList(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if d.listLimiter != nil {
		waitCtx, cancel := context.WithTimeout(ctx, baiduLimiterWaitTimeout)
		defer cancel()
		if err := d.listLimiter.acquireBackground(waitCtx); err != nil {
			return err
		}
		defer d.listLimiter.releaseBackground()
	}
	reqCtx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	rootDir := d.BackendPath("/")
	params := map[string]string{
		"method": "list",
		"dir":    rootDir,
		"web":    "web",
		"start":  "0",
		"limit":  "1",
	}
	if d.OrderBy != "" {
		params["order"] = d.OrderBy
		if d.OrderDirection == "desc" {
			params["desc"] = "1"
		}
	}
	_, err := d.get(reqCtx, "/xpan/file", params, nil)
	return err
}

func clampInt(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func clampInt64(v, min, max int64) int64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func megabytesToBytes(sizeMB, fallbackMB int64) int {
	if sizeMB <= 0 {
		sizeMB = fallbackMB
	}
	maxIntBytes := int64(^uint(0) >> 1)
	maxMB := maxIntBytes / int64(utils.MB)
	if maxMB <= 0 {
		maxMB = 1
	}
	if sizeMB > maxMB {
		sizeMB = maxMB
	}
	return int(sizeMB * int64(utils.MB))
}

func (d *BaiduNetdisk) currentDownloadRuntimeConfig() baiduDownloadRuntimeConfig {
	profile := currentBaiduMemoryProfile()
	backgroundLimit := d.DownloadTaskConcurrency
	if profile.downloadTaskConcurrency > 0 {
		backgroundLimit = minInt(backgroundLimit, profile.downloadTaskConcurrency)
	}
	if backgroundLimit <= 0 {
		backgroundLimit = 1
	}

	partConcurrency := d.downloadPartConcurrency
	if partConcurrency <= 0 {
		partConcurrency = defaultDownloadPartConcurrency
	}

	partSizeMB := d.DownloadPartSizeMB
	if partSizeMB <= 0 {
		partSizeMB = defaultDownloadPartSizeMB
	}

	return baiduDownloadRuntimeConfig{
		downloadTaskConcurrency: backgroundLimit,
		partConcurrency:         partConcurrency,
		partSize:                megabytesToBytes(partSizeMB, defaultDownloadPartSizeMB),
		proxyCacheBytes:         profile.proxyCacheBytes,
	}
}

func (d *BaiduNetdisk) refreshDownloadRuntimePolicy() baiduDownloadRuntimeConfig {
	cfg := d.currentDownloadRuntimeConfig()
	if d.downloadLimiter != nil {
		d.downloadLimiter.setLimit(cfg.downloadTaskConcurrency)
	}
	if d.proxyCache != nil && cfg.proxyCacheBytes > 0 {
		d.proxyCache.updateSoftMaxBytes(cfg.proxyCacheBytes, time.Now())
	}
	return cfg
}

func (d *BaiduNetdisk) ensureProxyCache(maxBytes int64) *baiduProxyRangeCache {
	if maxBytes <= 0 {
		return nil
	}
	if cache := d.proxyCache; cache != nil {
		return cache
	}
	d.proxyCacheInitMu.Lock()
	defer d.proxyCacheInitMu.Unlock()
	if d.proxyCache != nil {
		return d.proxyCache
	}
	cache := newBaiduProxyRangeCache(maxBytes, defaultBaiduProxyCacheTTL, true)
	if cache == nil {
		return nil
	}
	registerActiveBaiduProxyCache(cache)
	d.proxyCache = cache
	return cache
}

func (d *BaiduNetdisk) List(ctx context.Context, dir model.Obj, args model.ListArgs) ([]model.Obj, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if d.listLimiter != nil {
		waitCtx, cancel := context.WithTimeout(ctx, baiduLimiterWaitTimeout)
		defer cancel()
		if err := d.listLimiter.acquireInteractive(waitCtx); err != nil {
			return nil, err
		}
		defer d.listLimiter.releaseInteractive()
	}
	files, err := d.getFiles(ctx, dir.GetPath())
	if err != nil {
		return nil, err
	}
	return utils.SliceConvert(files, func(src File) (model.Obj, error) {
		return fileToObj(src), nil
	})
}

func (d *BaiduNetdisk) Link(ctx context.Context, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	link, err := d.linkOfficial(ctx, file, args)
	if err != nil {
		return nil, err
	}
	if args.HttpReq != nil {
		link.RequestRole = "interactive"
	}
	d.applyDownloadPolicy(link, file)
	return link, nil
}

func (d *BaiduNetdisk) resolveRequestRole(link *model.Link) requestRole {
	if link != nil {
		if strings.EqualFold(link.RequestRole, "interactive") {
			return requestRoleInteractive
		}
	}
	return requestRoleBackground
}

func (d *BaiduNetdisk) MakeDir(ctx context.Context, parentDir model.Obj, dirName string) (model.Obj, error) {
	var newDir File
	_, err := d.create(ctx, stdpath.Join(parentDir.GetPath(), dirName), 0, 1, "", "", &newDir, 0, 0)
	if err != nil {
		return nil, err
	}
	return fileToObj(newDir), nil
}

func cloneManagedObj(obj model.Obj, newPath, newName string) model.Obj {
	now := time.Now()
	switch src := model.UnwrapObj(obj).(type) {
	case *model.ObjThumbURL:
		cloned := *src
		cloned.Path = newPath
		cloned.Name = newName
		cloned.Modified = now
		return &cloned
	case *model.ObjThumb:
		cloned := *src
		cloned.Path = newPath
		cloned.Name = newName
		cloned.Modified = now
		return &cloned
	case *model.ObjectURL:
		cloned := *src
		cloned.Path = newPath
		cloned.Name = newName
		cloned.Modified = now
		return &cloned
	case *model.Object:
		cloned := *src
		cloned.Path = newPath
		cloned.Name = newName
		cloned.Modified = now
		return &cloned
	default:
		return &model.Object{
			ID:       obj.GetID(),
			Path:     newPath,
			Name:     newName,
			Size:     obj.GetSize(),
			Modified: now,
			Ctime:    obj.CreateTime(),
			IsFolder: obj.IsDir(),
			HashInfo: obj.GetHash(),
		}
	}
}

func (d *BaiduNetdisk) Move(ctx context.Context, srcObj, dstDir model.Obj) (model.Obj, error) {
	data := []base.Json{
		{
			"path":    srcObj.GetPath(),
			"dest":    dstDir.GetPath(),
			"newname": srcObj.GetName(),
		},
	}
	_, err := d.manage(ctx, "move", data)
	if err != nil {
		return nil, err
	}
	return cloneManagedObj(srcObj, stdpath.Join(dstDir.GetPath(), srcObj.GetName()), srcObj.GetName()), nil
}

func (d *BaiduNetdisk) Rename(ctx context.Context, srcObj model.Obj, newName string) (model.Obj, error) {
	data := []base.Json{
		{
			"path":    srcObj.GetPath(),
			"newname": newName,
		},
	}
	_, err := d.manage(ctx, "rename", data)
	if err != nil {
		return nil, err
	}
	return cloneManagedObj(srcObj, stdpath.Join(stdpath.Dir(srcObj.GetPath()), newName), newName), nil
}

func (d *BaiduNetdisk) Copy(ctx context.Context, srcObj, dstDir model.Obj) (model.Obj, error) {
	data := []base.Json{
		{
			"path":    srcObj.GetPath(),
			"dest":    dstDir.GetPath(),
			"newname": srcObj.GetName(),
		},
	}
	_, err := d.manage(ctx, "copy", data)
	if err != nil {
		return nil, err
	}
	return cloneManagedObj(srcObj, stdpath.Join(dstDir.GetPath(), srcObj.GetName()), srcObj.GetName()), nil
}

func (d *BaiduNetdisk) Remove(ctx context.Context, obj model.Obj) error {
	data := []string{obj.GetPath()}
	_, err := d.manage(ctx, "delete", data)
	return err
}

func (d *BaiduNetdisk) uploadProgressScopeKey() string {
	if d.Storage.ID > 0 {
		return fmt.Sprintf("storage:%d", d.Storage.ID)
	}
	if d.ClientID != "" {
		return "client:" + d.ClientID
	}
	return d.Config().Name
}

func toUploadProgressState(precreateResp *PrecreateResp) (baiduUploadProgressState, bool) {
	if precreateResp == nil || precreateResp.Uploadid == "" || len(precreateResp.BlockList) == 0 {
		return baiduUploadProgressState{}, false
	}
	pending := make([]uint32, 0, len(precreateResp.BlockList))
	for _, seq := range precreateResp.BlockList {
		if seq < 0 {
			continue
		}
		pending = append(pending, uint32(seq))
	}
	if len(pending) == 0 || len(pending) > maxCachedUploadPendingParts {
		return baiduUploadProgressState{}, false
	}
	return baiduUploadProgressState{
		Uploadid:       precreateResp.Uploadid,
		PendingPartSeq: pending,
	}, true
}

func fromUploadProgressState(state *baiduUploadProgressState) (*PrecreateResp, bool) {
	if state == nil || state.Uploadid == "" || len(state.PendingPartSeq) == 0 {
		return nil, false
	}
	blockList := make([]int, 0, len(state.PendingPartSeq))
	for _, seq := range state.PendingPartSeq {
		blockList = append(blockList, int(seq))
	}
	if len(blockList) == 0 {
		return nil, false
	}
	return &PrecreateResp{
		ReturnType: 1,
		Uploadid:   state.Uploadid,
		BlockList:  blockList,
	}, true
}

func (d *BaiduNetdisk) PutRapid(ctx context.Context, dstDir model.Obj, stream model.FileStreamer) (model.Obj, error) {
	contentMd5 := stream.GetHash().GetHash(utils.MD5)
	if len(contentMd5) < utils.MD5.Width {
		return nil, errors.New("invalid hash")
	}

	streamSize := stream.GetSize()
	path := stdpath.Join(dstDir.GetPath(), stream.GetName())
	mtime := stream.ModTime().Unix()
	ctime := stream.CreateTime().Unix()
	blockList, _ := utils.Json.MarshalToString([]string{contentMd5})

	var newFile File
	_, err := d.create(ctx, path, streamSize, 0, "", blockList, &newFile, mtime, ctime)
	if err != nil {
		return nil, err
	}
	// 修复时间，具体原因见 Put 方法注释的 **注意**
	newFile.Ctime = stream.CreateTime().Unix()
	newFile.Mtime = stream.ModTime().Unix()
	return fileToObj(newFile), nil
}

type baiduBlockListBuilder struct {
	buf    bytes.Buffer
	count  int
	closed bool
}

func newBaiduBlockListBuilder(parts int) *baiduBlockListBuilder {
	b := &baiduBlockListBuilder{}
	if parts > 0 {
		estimate := int64(parts)*36 + 2
		maxInt := int(^uint(0) >> 1)
		if estimate > int64(maxInt) {
			estimate = int64(maxInt)
		}
		if estimate > 0 {
			b.buf.Grow(int(estimate))
		}
	}
	b.buf.WriteByte('[')
	return b
}

func (b *baiduBlockListBuilder) AppendMD5(sum []byte) {
	if b == nil || b.closed {
		return
	}
	if b.count > 0 {
		b.buf.WriteByte(',')
	}
	b.buf.WriteByte('"')
	var hexBuf [32]byte
	hex.Encode(hexBuf[:], sum)
	b.buf.Write(hexBuf[:])
	b.buf.WriteByte('"')
	b.count++
}

func (b *baiduBlockListBuilder) String() string {
	if b == nil {
		return "[]"
	}
	if !b.closed {
		b.buf.WriteByte(']')
		b.closed = true
	}
	return b.buf.String()
}

type baiduUploadHashMeta struct {
	ContentMD5   string
	SliceMD5     string
	BlockListStr string
	PartCount    int
	LastPartSize int64
}

func (d *BaiduNetdisk) hashUploadParts(ctx context.Context, tempFile model.File, streamSize, sliceSize int64, updateProgress func(float64)) (baiduUploadHashMeta, error) {
	partCount := int(math.Max(math.Ceil(float64(streamSize)/float64(sliceSize)), 1))
	lastBlockSize := streamSize % sliceSize
	if streamSize > 0 && lastBlockSize == 0 {
		lastBlockSize = sliceSize
	}

	const FirstSliceSize int64 = 256 * 1024
	fileMd5H := md5.New()
	sliceMd5H := md5.New()
	sliceMd5H2 := md5.New()
	sliceMd5H2Write := utils.LimitWriter(sliceMd5H2, FirstSliceSize)
	blockListBuilder := newBaiduBlockListBuilder(partCount)
	byteSize := sliceSize

	for i := 1; i <= partCount; i++ {
		if utils.IsCanceled(ctx) {
			return baiduUploadHashMeta{}, ctx.Err()
		}
		if i == partCount {
			byteSize = lastBlockSize
		}
		_, err := utils.CopyWithBufferN(io.MultiWriter(fileMd5H, sliceMd5H, sliceMd5H2Write), tempFile, byteSize)
		if err != nil && err != io.EOF {
			return baiduUploadHashMeta{}, err
		}
		offset := int64(i-1) * sliceSize
		dropTempFileCacheRange(tempFile, offset, byteSize)
		blockListBuilder.AppendMD5(sliceMd5H.Sum(nil))
		sliceMd5H.Reset()
		if updateProgress != nil && partCount > 0 {
			// Hashing is unavoidable before precreate/upload; expose it as early progress.
			updateProgress(float64(i) * 10 / float64(partCount))
		}
	}

	return baiduUploadHashMeta{
		ContentMD5:   hex.EncodeToString(fileMd5H.Sum(nil)),
		SliceMD5:     hex.EncodeToString(sliceMd5H2.Sum(nil)),
		BlockListStr: blockListBuilder.String(),
		PartCount:    partCount,
		LastPartSize: lastBlockSize,
	}, nil
}

// Put
//
// **注意**: 截至 2024/04/20 百度云盘 api 接口返回的时间永远是当前时间，而不是文件时间。
// 而实际上云盘存储的时间是文件时间，所以此处需要覆盖时间，保证缓存与云盘的数据一致
func (d *BaiduNetdisk) Put(ctx context.Context, dstDir model.Obj, stream model.FileStreamer, up driver.UpdateProgress) (model.Obj, error) {
	if d.uploadLimiter != nil {
		if err := d.uploadLimiter.acquire(ctx); err != nil {
			return nil, err
		}
		defer d.uploadLimiter.release()
	}
	updateProgress := func(p float64) {
		if up != nil {
			up(task.NormalizeProgress(p))
		}
	}
	// rapid upload
	if newObj, err := d.PutRapid(ctx, dstDir, stream); err == nil {
		updateProgress(100)
		return newObj, nil
	}

	tempFile, err := stream.CacheFullInTempFile()
	if err != nil {
		return nil, err
	}
	if f, ok := tempFile.(fdFile); ok {
		_ = utils.DropFileCacheByFD(f.Fd(), 0, 0)
	}

	streamSize := stream.GetSize()
	sliceSize := d.getSliceSize()
	hashMeta, err := d.hashUploadParts(ctx, tempFile, streamSize, sliceSize, updateProgress)
	if err != nil {
		return nil, err
	}
	contentMd5 := hashMeta.ContentMD5
	sliceMd5 := hashMeta.SliceMD5
	blockListStr := hashMeta.BlockListStr
	path := stdpath.Join(dstDir.GetPath(), stream.GetName())
	mtime := stream.ModTime().Unix()
	ctime := stream.CreateTime().Unix()

	// step.1 预上传
	precreateResp := &PrecreateResp{}
	params := map[string]string{
		"method": "precreate",
	}
	form := map[string]string{
		"path":        path,
		"size":        strconv.FormatInt(streamSize, 10),
		"isdir":       "0",
		"autoinit":    "1",
		"rtype":       "3",
		"block_list":  blockListStr,
		"content-md5": contentMd5,
		"slice-md5":   sliceMd5,
	}
	joinTime(form, ctime, mtime)

	log.Debugf("[baidu_netdisk] precreate data: %s", form)
	_, err = d.postForm(ctx, "/xpan/file", params, form, &precreateResp)
	if err != nil {
		return nil, err
	}
	log.Debugf("%+v", precreateResp)
	if precreateResp.ReturnType == 2 {
		//rapid upload, since got md5 match from baidu server
		// 修复时间，具体原因见 Put 方法注释的 **注意**
		precreateResp.File.Ctime = ctime
		precreateResp.File.Mtime = mtime
		updateProgress(100)
		return fileToObj(precreateResp.File), nil
	}
	// step.2 上传分片
	totalParts := len(precreateResp.BlockList)
	threadG, upCtx := errgroup.NewGroupWithContext(ctx, d.uploadThread,
		retry.Attempts(3),
		retry.Delay(time.Second),
		retry.DelayType(retry.BackOffDelay))
	for i, partseq := range precreateResp.BlockList {
		if utils.IsCanceled(upCtx) {
			break
		}

		i, partseq, offset, byteSize := i, partseq, int64(partseq)*sliceSize, sliceSize
		if partseq+1 == hashMeta.PartCount {
			byteSize = hashMeta.LastPartSize
		}
		threadG.Go(func(ctx context.Context) error {
			params := map[string]string{
				"method":       "upload",
				"access_token": d.AccessToken,
				"type":         "tmpfile",
				"path":         path,
				"uploadid":     precreateResp.Uploadid,
				"partseq":      strconv.Itoa(partseq),
			}
			err := d.uploadSlice(ctx, params, stream.GetName(), io.NewSectionReader(tempFile, offset, byteSize), path, offset, byteSize)
			if err != nil {
				return err
			}
			dropTempFileCacheRange(tempFile, offset, byteSize)
			if totalParts > 0 {
				// Upload phase contributes the major part of visible progress.
				updateProgress(10 + float64(threadG.Success())*85/float64(totalParts))
			}
			precreateResp.BlockList[i] = -1
			return nil
		})
	}
	if err = threadG.Wait(); err != nil {
		return nil, err
	}

	// step.3 创建文件
	updateProgress(96)
	var newFile File
	_, err = d.create(ctx, path, streamSize, 0, precreateResp.Uploadid, blockListStr, &newFile, mtime, ctime)
	if err != nil {
		return nil, err
	}
	// 修复时间，具体原因见 Put 方法注释的 **注意**
	newFile.Ctime = ctime
	newFile.Mtime = mtime
	updateProgress(100)
	return fileToObj(newFile), nil
}

func (d *BaiduNetdisk) uploadSlice(ctx context.Context, params map[string]string, fileName string, file io.Reader, path string, offset, size int64) error {
	if ctx == nil {
		ctx = context.Background()
	}
	upCtx, cancel := context.WithTimeout(ctx, time.Hour)
	defer cancel()
	finishActive := beginBaiduTransferBlock(baiduTransferDirectionUpload)
	defer finishActive()
	startedAt := time.Now()
	res, err := base.RestyClient.R().
		SetContext(upCtx).
		SetQueryParams(params).
		SetHeader("User-Agent", "pan.baidu.com").
		SetHeader("Referer", "https://pan.baidu.com/").
		SetFileReader("file", fileName, file).
		Post(d.UploadAPI + "/rest/2.0/pcs/superfile2")
	if err != nil {
		return err
	}
	log.Debugln(res.RawResponse.Status + res.String())
	errCode := utils.Json.Get(res.Body(), "error_code").ToInt()
	errNo := utils.Json.Get(res.Body(), "errno").ToInt()
	if utils.SliceContains([]int{111, -6}, errNo) || utils.SliceContains([]int{111, -6}, errCode) {
		log.Info("refreshing baidu_netdisk token while uploading.")
		if err := d.refreshToken(ctx); err != nil {
			return err
		}
		return errs.NewErr(errs.StreamIncomplete, "baidu token expired while uploading, will retry")
	}
	if errCode != 0 || errNo != 0 {
		return errs.NewErr(errs.StreamIncomplete, "error in uploading to baidu, will retry. response=%s", res.String())
	}
	recordBaiduTransferBlock(baiduTransferDirectionUpload, baiduTransferBlock{
		BackendID:  d.BackendID(),
		File:       path,
		Offset:     offset,
		Size:       size,
		StartedAt:  startedAt,
		FinishedAt: time.Now(),
	})
	return nil
}

var _ driver.Driver = (*BaiduNetdisk)(nil)

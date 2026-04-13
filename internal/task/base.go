package task

import (
	"github.com/alist-org/alist/v3/internal/model"
	"github.com/xhofe/tache"
	"github.com/google/uuid"
)

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

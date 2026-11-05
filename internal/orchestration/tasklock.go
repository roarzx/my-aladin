package orchestration

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	redisstore "github.com/roarzx/my-aladin/internal/store/redis"
)

// ErrTaskLocked 表示任务已被其他 Runner 持有。
var ErrTaskLocked = errors.New("orchestration: 任务已被其他 Runner 持有")

// TaskLock 是任务级互斥锁（D7）：SET NX PX + 原子续约 + 原子释放。
type TaskLock struct {
	locker *redisstore.Locker
	keys   redisstore.Keys
	ttl    time.Duration
	renew  time.Duration
}

// NewTaskLock 构造任务锁；renew < 0 表示不自动续约（测试用）。
func NewTaskLock(client redis.UniversalClient, ttl, renew time.Duration) *TaskLock {
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	if renew == 0 {
		renew = 10 * time.Second
	}
	return &TaskLock{
		locker: redisstore.NewLocker(client),
		ttl:    ttl,
		renew:  renew,
	}
}

// Acquire 获取任务锁；成功返回 release 函数。
func (l *TaskLock) Acquire(ctx context.Context, taskID, owner string) (func(), error) {
	key := l.keys.TaskLock(taskID)
	ok, err := l.locker.Acquire(ctx, key, owner, l.ttl)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrTaskLocked
	}

	stop := make(chan struct{})
	if l.renew > 0 {
		go l.renewLoop(key, owner, stop)
	}

	var once sync.Once
	release := func() {
		once.Do(func() {
			close(stop)
			_, _ = l.locker.Release(context.Background(), key, owner)
		})
	}
	return release, nil
}

func (l *TaskLock) renewLoop(key, owner string, stop <-chan struct{}) {
	ticker := time.NewTicker(l.renew)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			ok, err := l.locker.Renew(context.Background(), key, owner, l.ttl)
			if err != nil || !ok {
				return
			}
		}
	}
}

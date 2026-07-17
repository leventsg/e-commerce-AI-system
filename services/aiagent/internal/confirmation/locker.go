package confirmation

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/zeromicro/go-zero/core/stores/redis"
)

var ErrConfirmationLockNotOwned = errors.New("confirmation lock is no longer owned")

type Lock interface {
	Release(ctx context.Context) error
}

type Locker interface {
	Acquire(ctx context.Context, key string, ttl time.Duration) (Lock, bool, error)
}

type RedisLocker struct {
	store *redis.Redis
}

func NewRedisLocker(store *redis.Redis) *RedisLocker {
	return &RedisLocker{store: store}
}

// Acquire 获取确认锁
func (l *RedisLocker) Acquire(ctx context.Context, key string, ttl time.Duration) (Lock, bool, error) {
	lock := redis.NewRedisLock(l.store, key)
	seconds := int(math.Ceil(ttl.Seconds()))
	if seconds < 1 {
		seconds = 1
	}
	lock.SetExpire(seconds)
	acquired, err := lock.AcquireCtx(ctx)
	if err != nil || !acquired {
		return nil, acquired, err
	}
	return redisLock{lock: lock}, true, nil
}

type redisLock struct {
	lock *redis.RedisLock
}

// Release 释放确认锁
func (l redisLock) Release(ctx context.Context) error {
	released, err := l.lock.ReleaseCtx(ctx)
	if err != nil {
		return err
	}
	if !released {
		return ErrConfirmationLockNotOwned
	}
	return nil
}

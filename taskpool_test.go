// Copyright 2021 gotomicro
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package taskpool

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"golang.org/x/sync/errgroup"
)


func TestConcurrentBlockedTaskPool_NewConcurrentBlockedTaskPool(t *testing.T) {
	t.Parallel()
	t.Run("New", func(t *testing.T) {
		t.Parallel()
		// queueSize 传参问题校验
		pool, err := NewConcurrentBlockedTaskPool(1, -1)
		assert.ErrorIs(t, err, errInvalidArgument)
		assert.Nil(t, pool)

		pool, err = NewConcurrentBlockedTaskPool(1, 0)
		assert.ErrorIs(t, err, errInvalidArgument)
		assert.Nil(t, pool)

		pool, err = NewConcurrentBlockedTaskPool(1, 1)
		assert.NoError(t, err)
		assert.NotNil(t, pool)

		// initGo 传参问题校验
		pool, err = NewConcurrentBlockedTaskPool(-1, 1)
		assert.ErrorIs(t, err, errInvalidArgument)
		assert.Nil(t, pool)

		pool, err = NewConcurrentBlockedTaskPool(0, 1)
		assert.ErrorIs(t, err, errInvalidArgument)
		assert.Nil(t, pool)

		pool, err = NewConcurrentBlockedTaskPool(1, 1)
		assert.NoError(t, err)
		assert.NotNil(t, pool)

	})

	t.Run("Submit", func(t *testing.T) {
		t.Parallel()
		t.Run("提交非法Task", func(t *testing.T) {
			t.Parallel()
			pool, _ := NewConcurrentBlockedTaskPool(1, 1)
			assert.Equal(t, stateCreated, pool.internalState())
			assert.ErrorIs(t, pool.Submit(context.Background(), nil), errTaskIsInvalid)
			assert.Equal(t, stateCreated, pool.internalState())
			assert.Equal(t, int32(1), pool.currentGo)
		})
	})

	t.Run("正常提交Task", func(t *testing.T) {
		t.Parallel()
		pool, _ := NewConcurrentBlockedTaskPool(1, 1)
		assert.Equal(t, stateCreated, pool.internalState())

		err := pool.Submit(context.Background(), TaskFunc(func(ctx context.Context) error { return nil }))
		assert.NoError(t, err)
		assert.Equal(t, stateRunning, pool.internalState())
		assert.Equal(t, int32(1), pool.currentGo)

		err = pool.Submit(context.Background(), TaskFunc(func(ctx context.Context) error { panic("task panic") }))
		assert.NoError(t, err)
		assert.Equal(t, stateRunning, pool.internalState())
		assert.Equal(t, int32(1), pool.currentGo)

		err = pool.Submit(context.Background(), TaskFunc(func(ctx context.Context) error { return errors.New("fake error") }))
		assert.NoError(t, err)
		assert.Equal(t, stateRunning, pool.internalState())
		assert.Equal(t, int32(1), pool.currentGo)

	})

	t.Run("超时提交", func(t *testing.T) {
		t.Parallel()

		pool, _ := NewConcurrentBlockedTaskPool(1, 1)
		assert.Equal(t, stateCreated, pool.internalState())

		done := make(chan struct{})
		err := pool.Submit(context.Background(), TaskFunc(func(ctx context.Context) error {
			<-done
			return nil
		}))
		assert.NoError(t, err)

		n := cap(pool.tasks) + 2
		eg := errgroup.Group{}

		for i := 0; i < n; i++ {
			eg.Go(func() error {
				ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
				defer cancel()
				return pool.Submit(ctx, TaskFunc(func(ctx context.Context) error {
					<-done
					return nil
				}))

			})
		}
		assert.ErrorIs(t, eg.Wait(), context.DeadlineExceeded)
	})

}

func TestConcurrentBlockedTaskPool_ShutdownNow(t *testing.T) {
	t.Parallel()
	t.Run("ShutdownNow-in-created", func(t *testing.T) {
		t.Parallel()
		pool, _ := NewConcurrentBlockedTaskPool(1, 1)
		assert.Equal(t, stateCreated, pool.internalState())

		// created->stopped
		tasks, err := pool.ShutdownNow()
		assert.NoError(t, err)
		assert.Equal(t, 0, len(tasks))
		assert.Equal(t, stateStopped, pool.internalState())
	})

	t.Run("ShutdownNow-in-running", func(t *testing.T) {
		t.Parallel()
		pool, _ := NewConcurrentBlockedTaskPool(1, 1)
		assert.Equal(t, stateCreated, pool.internalState())

		err := pool.Submit(context.Background(), TaskFunc(func(ctx context.Context) error {
			return nil
		}))
		assert.NoError(t, err)
		assert.Equal(t, stateRunning, pool.state)

		// running -> stopped
		tasks, err := pool.ShutdownNow()
		assert.NoError(t, err)
		assert.Equal(t, 0, len(tasks))
		assert.Equal(t, stateStopped, pool.internalState())
	})

	t.Run("ShutdownNow-in-stoped", func(t *testing.T) {
		t.Parallel()
		pool, _ := NewConcurrentBlockedTaskPool(1, 1)
		assert.Equal(t, stateCreated, pool.internalState())

		// running -> clossing-> stopped
		tasks, err := pool.ShutdownNow()
		assert.NoError(t, err)
		assert.Equal(t, 0, len(tasks))
		// 确认是 stopped
		assert.Equal(t, stateStopped, pool.internalState())

		// 再次关闭
		_, err = pool.ShutdownNow()
		assert.ErrorIs(t, err, errTaskPoolIsStopped)

	})
}

func TestConcurrentBlockedTaskPool_Shutdown(t *testing.T) {
	t.Parallel()
	t.Run("ShutdownNow-in-created", func(t *testing.T) {
		t.Parallel()
		pool, _ := NewConcurrentBlockedTaskPool(1, 1)
		assert.Equal(t, stateCreated, pool.internalState())

		// created->stopped
		done, err := pool.Shutdown()
		assert.NotNil(t, done)
		assert.Nil(t, err)
		<-done
		assert.Equal(t, stateStopped, pool.internalState())
	})

	t.Run("ShutdownNow-in-running", func(t *testing.T) {
		t.Parallel()
		pool, _ := NewConcurrentBlockedTaskPool(1, 1)
		assert.Equal(t, stateCreated, pool.internalState())

		err := pool.Submit(context.Background(), TaskFunc(func(ctx context.Context) error {
			return nil
		}))
		assert.NoError(t, err)
		assert.Equal(t, stateRunning, pool.state)

		// running -> stopped
		done, err := pool.Shutdown()
		assert.NotNil(t, done)
		assert.Nil(t, err)
		<-done
		assert.Equal(t, stateStopped, pool.internalState())
	})

	t.Run("ShutdownNow-in-stoped", func(t *testing.T) {
		t.Parallel()
		pool, _ := NewConcurrentBlockedTaskPool(1, 1)
		assert.Equal(t, stateCreated, pool.internalState())

		// running -> clossing-> stopped
		done, err := pool.Shutdown()
		assert.NotNil(t, done)
		assert.Nil(t, err)
		<-done

		// 确认是 stopped
		assert.Equal(t, stateStopped, pool.internalState())

		// 再次关闭
		done, err = pool.Shutdown()
		assert.Nil(t, done)
		assert.ErrorIs(t, err, errTaskPoolIsStopped)

	})
}

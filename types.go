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
	"fmt"
	"runtime"
)

// Task 代表一个任务
type Task interface {
	// Run 执行任务
	// 如果 ctx 设置了超时时间，那么实现者需要自己决定是否进行超时控制
	Run(ctx context.Context) error
}

// 让用户自己利用函数式编程，解决传参、获取结果之类的问题，比如利用channel
type TaskFunc func(ctx context.Context) error

// Run 执行任务
func (t TaskFunc) Run(ctx context.Context) (err error) {
	defer func() {
		// recover
		if r := recover(); r != nil {
			buf := make([]byte, panicBuffLen)
			buf = buf[:runtime.Stack(buf, false)]
			err = fmt.Errorf("%w:%s", errTaskRunningPanic, fmt.Sprintf("[PANIC]:\t%+v\n%s\n", r, buf))
		}
	}()
	return t(ctx)
}

// TaskPool 任务池
type TaskPool interface {
	// Submit 提交一个任务
	// 提交完任务就会立刻执行
	Submit(ctx context.Context, task Task) error

	// Shutdown 关闭任务池（优雅退出）
	// 任务池不可以在提交任务，但是会把剩余的任务执行完毕
	// 任务执行完毕后，会关闭返回的chan
	// 用户可以根据返回的chan判断任务池是否优雅退出
	Shutdown() (<-chan struct{}, error)

	// ShutdownNow 立刻停止，剩余任务不再执行
	ShutdownNow() ([]Task, error)
}

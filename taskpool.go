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
)

// 让用户自己利用函数式编程，解决传参、获取结果之类的问题，比如利用channel
type Task func()

type TaskPool struct {
	// 将任务缓存起来
	tasks chan Task
	close chan struct{}
}

// NewTaskPool 创建一个任务池
// numG 就是任务池要启用多少个goroutine来并发处理任务
func NewTaskPool(maxG int, capacity int) (*TaskPool, error) {
	res := &TaskPool{
		tasks: make(chan Task, capacity),
		close: make(chan struct{}),
	}
	for i := 0; i < maxG; i++ {
		go func() {
			for {
				select {
				case t := <-res.tasks:
					// fmt.Println("开始处理任务啦")
					t()
					// fmt.Println("处理任务结束")
				// 控制goroutine退出
				case <-res.close:
					return
				}
			}
		}()
	}
	return res, nil
}

// Submit 提交任务
func (p *TaskPool) Submit(ctx context.Context, t Task) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case p.tasks <- t:
	}
	return nil
}

// Close 关闭任务池，释放资源，所有goroutine都应该退出
// 重复调用会panic 不要重复调用
func (p *TaskPool) Close() error {
	close(p.close)
	return nil
}

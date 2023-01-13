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

/*
TaskPool有限状态机
    NewConcurrentBlockedTaskPool          Submit                 Shutdown/ShutdownNow
    			|                          |                             |
                | >                        | >                           |>
            stateCreated -------------> stateRunning ---------------> stateClosing -----------------> stateStopped
			    |                           |                                                              |
				|                           |                                                              |
				|----------------------------------------Shutdown/ShutdownNow------------------------------>
*/

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
)

var (
	stateCreated int32 = 0
	stateRunning int32 = 1
	stateClosing int32 = 2
	stateStopped int32 = 3
	stateLocked  int32 = 4

	_ TaskPool = &ConcurrentBlockedTaskPool{}

	errTaskPoolIsClosing = errors.New("ekit：TaskPool关闭中")
	errTaskPoolIsStopped = errors.New("ekit: TaskPool已停止")
	errTaskIsInvalid     = errors.New("ekit: Task非法")
	errTaskRunningPanic  = errors.New("ekit: Task运行时异常")

	errInvalidArgument = errors.New("ekit: 参数非法")

	panicBuffLen = 2048
)

// ConcurrentBlockedTaskPool 按需创建goroutine的并发阻塞的任务池
type ConcurrentBlockedTaskPool struct {
	// TaskPool当前状态
	state int32

	mutex sync.RWMutex

	// 将任务缓存起来
	tasks chan Task

	shutdownOnce sync.Once

	// 返回给外部用户的关闭信号
	shutdowDone chan struct{}

	// 内部中断信号
	shutdownNowCtx    context.Context
	shutdownNowCancel context.CancelFunc

	// 设置int32是为了使用atomic
	// 初始协程数
	initGo int32

	// 最大协程数
	maxGo int32

	//当前协程个数
	currentGo int32

	wg sync.WaitGroup

	// 协程id 递增
	idx int32
}

// NewConcurrentBlockedTaskPool 创建一个按需分配的任务池
// initGo 初始协程数
// queueSize 是队列大小，即最多有多少个任务在等待调度
func NewConcurrentBlockedTaskPool(initGo int, queueSize int) (*ConcurrentBlockedTaskPool, error) {
	if initGo < 1 {
		return nil, fmt.Errorf("%w：initGo应该大于0", errInvalidArgument)
	}
	if queueSize <= 0 {
		return nil, fmt.Errorf("%w：queueSize应该大于0", errInvalidArgument)
	}
	res := &ConcurrentBlockedTaskPool{
		tasks:       make(chan Task, queueSize),
		shutdowDone: make(chan struct{}, 1), //缓冲为1，以免用户没有读取导致阻塞
		initGo:      int32(initGo),
		maxGo:       int32(initGo),
		currentGo:   0,
	}
	res.shutdownNowCtx, res.shutdownNowCancel = context.WithCancel(context.Background())
	// 初始就启动 initGo个协程
	for i := 0; i < initGo; i++ {
		res.wg.Add(1)
		go res.goroutine(atomic.AddInt32(&res.idx, 1))
		atomic.AddInt32(&res.currentGo, 1)
	}
	atomic.StoreInt32(&res.state, stateCreated)
	return res, nil
}

// goroutine 启动一个协程
func (p *ConcurrentBlockedTaskPool) goroutine(id int32) {
	defer func() {
		p.wg.Done()
		atomic.AddInt32(&p.currentGo, -1)
	}()

	for {
		select {
		case task, ok := <-p.tasks:
			if !ok {
				// p.tasks 关闭，任务已经取完
				if atomic.CompareAndSwapInt32(&p.currentGo, 1, 0) && atomic.LoadInt32(&p.state) == stateClosing {
					// 如果当前协程是最后一个要发关闭信号给外界用户
					p.shutdownOnce.Do(func() {
						p.shutdowDone <- struct{}{}
						close(p.shutdowDone)
						atomic.CompareAndSwapInt32(&p.state, stateClosing, stateStopped)
					})
				}
				return
			}
			// 执行任务 用户自己决定是否需要处理退出信号等
			task.Run(p.shutdownNowCtx)

		case <-p.shutdownNowCtx.Done():
			// 监控内部退出信号，控制goroutine退出
			return
		}
	}
}

// Submit 提交任务
// 如果此时队列已满，那么将会阻塞调用者。
// 如果因为 ctx 的原因返回，那么将会返回 ctx.Err()
func (p *ConcurrentBlockedTaskPool) Submit(ctx context.Context, task Task) error {
	if task == nil {
		return fmt.Errorf("%w", errTaskIsInvalid)
	}
	for {
		if atomic.LoadInt32(&p.state) == stateClosing {
			return fmt.Errorf("%w", errTaskPoolIsClosing)
		}
		if atomic.LoadInt32(&p.state) == stateStopped {
			return fmt.Errorf("%w", errTaskPoolIsStopped)
		}
		state := atomic.LoadInt32(&p.state)
		if atomic.CompareAndSwapInt32(&p.state, state, stateLocked) {
			select {
			case <-ctx.Done():
				// 超时，比如队列已经满了
				return fmt.Errorf("%w", ctx.Err())
			case p.tasks <- task:
				// 提交任务成功，此时需要看是否需要新开goroutine
				p.mutex.Lock()
				// 说明有等待任务
				if p.currentGo < p.maxGo && len(p.tasks) > 0 {
					p.wg.Add(1)
					p.currentGo++
					go p.goroutine(atomic.AddInt32(&p.idx, 1))
				}
				p.mutex.Unlock()
				// 进入到这里，说明状态肯定不是stateClosing、stateStopped，修改为运行态stateRunning，原来有可能是stateCreated、stateRunning
				atomic.CompareAndSwapInt32(&p.state, stateLocked, stateRunning)
				return nil
			default:
				// 不能阻塞在临界区，因为此时用户可能需要Shutdown和ShutdownNow
				atomic.CompareAndSwapInt32(&p.state, stateLocked, state)
			}
		}

	}
}

// Shutdown 关闭任务池，释放资源，所有goroutine都应该退出
func (p *ConcurrentBlockedTaskPool) Shutdown() (<-chan struct{}, error) {
	for {
		if atomic.LoadInt32(&p.state) == stateCreated {
			// 让初始创建的initGo个协程退出
			p.shutdowDone <- struct{}{}
			for atomic.CompareAndSwapInt32(&p.state, stateCreated, stateStopped) {
			}
			return p.shutdowDone, nil
		}
		if atomic.LoadInt32(&p.state) == stateClosing {
			return nil, errTaskPoolIsClosing
		}

		if atomic.LoadInt32(&p.state) == stateStopped {
			return nil, errTaskPoolIsStopped
		}
		if atomic.CompareAndSwapInt32(&p.state, stateRunning, stateClosing) {
			// 此时不再接收新的任务，老任务会被执行完毕
			close(p.tasks)
			return p.shutdowDone, nil
		}
	}

}

// ShutdownNow 立即停止所有任务，关闭任务池，释放资源
func (p *ConcurrentBlockedTaskPool) ShutdownNow() ([]Task, error) {
	for {
		if atomic.LoadInt32(&p.state) == stateClosing {
			return nil, errTaskPoolIsClosing
		}

		if atomic.LoadInt32(&p.state) == stateStopped {
			return nil, errTaskPoolIsStopped
		}
		if atomic.CompareAndSwapInt32(&p.state, stateRunning, stateStopped) || atomic.CompareAndSwapInt32(&p.state, stateCreated, stateStopped) {
			// 目标：立刻关闭并且返回所有剩下未执行的任务
			// 策略：关闭等待队列不再接受新任务，中断工作协程的获取任务循环，清空等待队列并保存返回
			close(p.tasks)

			// 发送内部中断信号，立即停止所有任务
			p.shutdownNowCancel()

			tasks := make([]Task, 0, len(p.tasks))
			for task := range p.tasks {
				tasks = append(tasks, task)
			}
			p.wg.Wait()
			return tasks, nil
		}
	}
}

func (p *ConcurrentBlockedTaskPool) internalState() int32 {
	var n int32
	p.mutex.RLock()
	n = p.state
	p.mutex.RUnlock()
	return n
}

// numOfGo 当前任务池起了多少个goroutine
func (p *ConcurrentBlockedTaskPool) numOfGo() int32 {
	var n int32
	p.mutex.RLock()
	n = p.currentGo
	p.mutex.RUnlock()
	return n
}

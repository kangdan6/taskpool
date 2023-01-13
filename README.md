# taskpool
利用channel实现一个任务池

## 一些设计思路
1、提交任务时，如果缓存满了，是阻塞住提交者还是超时退出呢，我选择由用户自己确定，如果用户传入了超时控制，如果满了就超时退出，如果用户没有传入超时控制，就阻塞重试， 我觉得这种方式更优雅，用户可以自己选择是否重试

2、缓存应该设置多大？这个问题作为服务提供者我们依然无法抉择，所以这个问题还是交给用户自己抉择，通过传参的方式设置缓存大小queueSize

3、是新建缓存池的时候，采用饥饿模式直接新建处理任务的goroutine， 还是采用饱汉模式处理任务的时候再新建goroutine, 我比较倾向于按需创建，具体算法如下

    3.1、设计初始goroutine数量和最大goroutine数，任务池创建的时候就新建 初始个数的goroutine, 后面再创建任务的时候，如果没有空闲goroutine就创建一个goroutine,直到达到最大goroutine数。

TODO:
1、增加最大空闲时间，超过最大空闲时间，并且 currentGo > initGo， 就退出当前协程

## 有限状态机
```
    NewConcurrentBlockedTaskPool          Submit                 Shutdown/ShutdownNow
    			|                          |                             |
                | >                        | >                           |>
            stateCreated -------------> stateRunning ---------------> stateClosing -----------------> stateStopped
			    |                           |                                                              |
				|                           |                                                              |
				|----------------------------------------Shutdown/ShutdownNow------------------------------>
```
## 核心接口设计
```
Shutdown 开始优雅关闭

ShutdownNow 立即关闭

Submit 提交任务，提交成功立刻执行任务
```

## 提交任务
```
type TaskFunc func(ctx context.Context) error
```
异常捕获是通过衍生于TaskFunc的方法Run实现的：
```
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
```


## 实现细节
1、任务执行可能会panic, 需要recover住

2、提交任务后，如果有空闲goroutine就立刻执行

3、提交任务时是否超时退出，由用户决定（通过ctx）

4、ShutdownNow 立刻退出，通过内部终止信号通知所有协程

5、Shutdown 优雅退出，向用户返回一个chan，以判断是否关闭成功，最后一个协程负责向外发送关闭信号

6、已经创建未运行 和 运行态都可以关闭，具体关闭动作有些许差异


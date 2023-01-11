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
	"testing"
)

// BenchmarkTaskPool_NewTaskPool
// go test -benchmem -run=^$ -tags e2e -bench ^BenchmarkTaskPool_NewTaskPool$ taskpool
// goos: darwin
// goarch: arm64
// pkg: taskpool
// BenchmarkTaskPool_NewTaskPool-8   	  153766	      7264 ns/op	    2184 B/op	      37 allocs/op
// PASS
// ok  	taskpool	1.571s
func BenchmarkTaskPool_NewTaskPool(b *testing.B) {
	for i := 0; i < b.N; i++ {
		paramChan := make(chan Param, 10)
		resChan := make(chan Result, 10)
		for i := 0; i < 10; i++ {
			paramChan <- Param{num: i}
		}
		taskpool, err := NewTaskPool(10, 100)
		if err != nil {
			return
		}
		for i := 0; i < 10; i++ {
			taskpool.Submit(context.Background(), func() {
				param := <-paramChan
				// 一堆逻辑处理
				resChan <- Result{
					result: fmt.Sprintf("消费了消息：%v", param.num),
				}
			})
		}

		for i := 0; i < 10; i++ {
			<-resChan
		}
		taskpool.Close()
	}

}

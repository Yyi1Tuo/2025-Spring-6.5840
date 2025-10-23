### Lab 1 MapReduce

目标：

遇到的问题，在启动coordinator（以下都写作master）时这么写为什么不对？

```go
func MakeCoordinator(files []string, nReduce int) *Coordinator {
	c := Coordinator{
		nWorker:  nReduce,
		TaskMap:  make(map[int]*Task),
		TaskChan: make(chan *Task),
	}
	// Your code here.
	for i, v := range files {
		task := c.MakeTask(v, i)
		c.TaskMap[task.Taskid] = task
		c.TaskChan <- task
	}
	c.server()
	return &c
}
```

问题就在于这个信道，由于设置的信道是不带缓冲的，填入第一个task后就会阻塞进程，但是此时服务器还没有启动，worker永远不可能拿到任务了、



如何检测各worker的状态以便判断是否要进入下一个阶段，可以来一个for循环不断检测各task的状态，为了优雅，我们可以引入waitgroup，创建一个辅助线程来监视各task状态。

```go
wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			flag := true
			TaskLock.Lock()
			for task, state := range c.TaskMapState {
				if state == MapPhase {
					fmt.Println("Map阶段未结束", task, state)
					flag = false
					break
				}
			}
			TaskLock.Unlock()
			if flag {
				//Map阶段结束了
				break
			}
			time.Sleep(1 * time.Second)
		}

	}()

	wg.Wait()
```



但是上述操作引入一个严重的bug，注意看下面的代码，worker执行完任务后会调用report函数call一个master的函数来mark任务状态数组为已完成：

```go
//worker端
func reportTaskDone(Task *Task) {
	args := ReportTaskDoneArgs{Task: Task}
	replys := ReportTaskDoneReply{}
	ok := call("Coordinator.MarkTaskDone", &args, &replys)
	if ok {
		fmt.Printf("Report Task Done %v\n", Task.Taskid)
	} else {
		fmt.Printf("Report Task Done failed!\n")
	}
}
//master端
func (c *Coordinator) MarkTaskDone(args *ReportTaskDoneArgs, reply *ReportTaskDoneReply) error {
	TaskLock.Lock()
	defer TaskLock.Unlock()
	c.TaskMapState[args.Task] = DoneTask
	fmt.Println("MarkTaskDone", args.Task, c.TaskMapState[args.Task])
	return nil
}
//rpc定义
type ReportTaskDoneArgs struct {
	Task *Task
}
type ReportTaskDoneReply struct {
}
```

这里隐含了一个严重的错误，RPC是跨进程通信，不同进程有独立的内存空间，指针地址在另一个进程中没有意义，这就导致RPC不会也不能传递"相同地址"的指针。

gob 在解码的时候会 **新建一个 `Task` 对象**，然后返回它的指针给 `MarkTaskDone`。所以虽然里面的内容（`Taskid`、`TaskType` 等字段）是一样的，但指针地址和你最初 `MakeMapTask` 时放进 `TaskMapState` 的指针不同。

让我们看看rpc/gob的文档怎么说的：

1. **Gob编码的基本原理**：

> "Pointers are not transmitted, but the things they point to are transmitted; that is, the values are flattened." [gob package - encoding/gob - Go Packages](https://pkg.go.dev/encoding/gob)

这句话明确说明了：**指针本身不会被传输，只有指针指向的值会被传输**。

2. **解码时重新分配内存**：

> "In general, if allocation is required, the decoder will allocate memory. If not, it will update the destination variables with values read from the stream." [gob package - encoding/gob - Go Packages](https://pkg.go.dev/encoding/gob)

这说明解码器会**重新分配内存**来创建新对象。

3. **RPC使用gob进行编码**：

> "A typical use is transporting arguments and results of remote procedure calls (RPCs) such as those provided by net/rpc."

修改的方法有二，一是修改数据结构，key改为taskid，但这样任务完成后可能需要删除，以防和后面的reducetask冲突；二是根据taskid，回头找到master的原地址信息进行修改。



#### 容错机制的设计

基本的思路如下：coordinator不停发送heartbeat，并维护一个map记录收到回复的时间，隔一段时间检查这个map，如果迟迟没有回复则认为worker已宕机，需要重新部署任务。

> 由于在此lab中，worker完成一次任务的时间并不长，我们甚至不用让worker一直发送heartbeat，只需记录任务起始时间，超时则任务宕机。



### Worker的退出

还有一个比较难处理的细节是，如何通知worker退出，或者收worker如何才能判断master的退出无用的dialing。虽然说不影响结果的正确性但是看着满屏幕的connection fail实在使人心烦。

#### 分布式中文件操作的Trick

假设一个线程需要写入一个文件，为了防止其崩溃导致一个不可用的中间文件被其他线程误读，我们可以使用临时文件+重命名的方法，在大部分的UNIX文件系统中`os.Rename(old, new)` 都是**原子操作**，这也就保证不会出现错误的文件被读取。

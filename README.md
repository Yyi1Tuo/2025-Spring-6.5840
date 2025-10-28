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



#### Worker的退出

还有一个比较难处理的细节是，如何通知worker退出，或者收worker如何才能判断master的退出无用的dialing。虽然说不影响结果的正确性但是看着满屏幕的connection fail实在使人心烦。

我们可以在master退出前，发布一组伪任务用于告知worker退出

```go
//发布Please Exit任务，防止出现dialing error或者Unexpected EOF
for i := 0; i < nReduce; i++ { //当然实际上加入worker的数量大于reduce数量时，这种写法还是会有问题，
// 但是如果我们从mapreduce的设计出发，worker的数量应该大于reduce数量，否则reduce阶段无法进行，所以这里可以认为是没有问题的
		exittask := &Task{
			Taskid:    i,
			TaskType:  DoneTask,
			Filename:  "",
			ReduceNum: nReduce,
		}
		c.TaskChan <- exittask
	}
	time.Sleep(3 * time.Second)
	return &c
```

#### 分布式中文件操作的Trick

假设一个线程需要写入一个文件，为了防止其崩溃导致一个不可用的中间文件被其他线程误读，我们可以使用临时文件+重命名的方法，在大部分的UNIX文件系统中`os.Rename(old, new)` 都是**原子操作**，这也就保证不会出现错误的文件被读取。





### Lab2 K/V Server

2025 Spring 的lab2与2024年的有很大不同，2024只是一个简单的linearizability的KV server，只需为请求加上seq即可；2025在linearizabilty的同时要实现most at once语意，并利用这个KV服务器实现锁的功能。

> 然而事实上25年的lab要更简单，因为代码中的注释和结构体非常清晰，只需要按照说明补全即可，而24的版本需要自己设计，容易出现各种各样的bug

#### Version的粒度

lab2代码中的注释强调应该为每一个KV对设置一个version号，有的同学会问为什么不为整个server设置一个version号，还能更加节约内存。

这里涉及到的问题是粒度的选择。首先我们考虑linearizability应该是对应server的还是KV对的？假设我们要维护整个server的全局线性一致性，那么意味着所有写入操作都必须串行化，任何一个 key 的写操作都会增加版本号，导致明明修改无关的key也会受到影响，性能会极大下降。
与此同时，这种server的线性一致性对于使用来说完全没有必要，我们只需要保证对每一个KV对来说，所有用户的读写操作具有一致性即可，并且对每一对KV对设置一个uint64的版本号消耗的内存并没有想象中的大。

#### 利用KV Server实现Lock

我们利用KV对来模拟一个lock，用key存储lockname，value存储持有者。
由于我们实现的KV Server是不可靠传输，所以在使用时要根据返回的ErrType自行处理以保证可靠的互斥锁。

```go
func (lk *Lock) Acquire() {
	// Your code here
	for {
		//使用get来获取锁的状态
		locker, ver, err := lk.ck.Get(lk.lockName)
		if err == rpc.ErrNoKey {
			//没有锁，直接创建锁
			lockErr := lk.ck.Put(lk.lockName, lk.clientID, 0)
			//我们需要判断是不是真的锁上了
			switch lockErr {
			case rpc.OK:
				return
			case rpc.ErrVersion:
				//版本不匹配，可能是别人持有锁，可能是先前的请求已经送到，由于网络故障没有收到回复
				//我们选择重试
			case rpc.ErrMaybe:
				//不确定,需要检查是不是真的锁上了
				locker, _, _ := lk.ck.Get(lk.lockName)
				if locker == lk.clientID {
					return
				}
				//说明put没生效,重试
			default:
			}

		} else {
			//锁存在
			switch locker {
			case lk.clientID: //已经是自己持有锁
				return
			case "": //没有持有者，直接持有锁
				lockErr := lk.ck.Put(lk.lockName, lk.clientID, ver)
				switch lockErr {
				case rpc.OK:
					return
				case rpc.ErrVersion:
				case rpc.ErrMaybe:
					locker, _, _ := lk.ck.Get(lk.lockName)
					if locker == lk.clientID {
						return
					}
				default:
				}
			default:
				// 其它持有者，稍后重试
			}
		}
		//防止livelock，需要一个随机退避
		time.Sleep(time.Duration(nrand()%100+50) * time.Millisecond)
	}

}
```



### Lab 3 RAFT

#### Part A

这一部分主要要求我们实现有关选举部分的内容，每个Raft实例都应该维护两个超时器，一个控制选举超时，一个控制心跳间隔（仅对Leader有意义）。Go的Time库中有两个有关定时器的对象，一个是timer，一个是ticker。根据题意我们很容易写出这样的代码：

```go
func (rf *Raft) ticker() {
	for rf.killed() == false {
		// Check if a leader election should be started.
		select {
		case <-rf.timer.C:
			rf.mu.Lock()
			if rf.state == Leader {
				rf.mu.Unlock()
				rf.broadcastHeartbeat()
				rf.timer.Reset(100 * time.Millisecond)
			} else {
				rf.mu.Unlock()
				DPrintf("Server %d Election Timeout, start election", rf.me)
				rf.startElection()
				rf.timer.Reset(getRandomElectionTimeout())
			}
		}
	}
}
```

这样的写法问题在哪，我们首先明确一下timer的原理，本质上是一个channel：
```go
type Timer struct {
    C <-chan Time  // 定时到达时，会向这个通道发一个值
}
```

当定时器超时，它往通道 `C` 里塞一个信号。如果你没把这个信号读出来，那信号就**一直留在通道里**。如果你此时调用 `Reset()`，它只是**重新设置时间**，但不会自动清空那个旧信号。这就会导致即使server收到心跳reset定时器，可能还是会触发旧的超时信号导致重新选举。

在助教的建议里也明确说了：

> Don't use time.Ticker and time.Timer; they are tricky to use correctly.

#### 实现细节

有一个细节，虽然不影响正确性，但是会影响选举时间的问题：你可能认为——只要收到一个 `AppendEntries` 或 `RequestVote` RPC，就应该重置选举计时器。毕竟，这两种消息都意味着其他节点要么认为自己是领导者，要么正试图成为领导者。从直觉上看，这意味着我们当前这个节点“暂时不该干扰”（即不该发起选举）。

然而，如果你仔细阅读 **图 2**，它的原文写的是：

> 如果选举超时经过，且在此期间没有收到来自**“当前领导者”**的 RPC，也**没有给任何候选人投票**，则转换为候选人。

这种看似细微的区别其实影响很大：如果你在实现中采用“只要收到任何 RPC 就重置选举计时器”的逻辑，那么在某些情况下，系统的**活性（liveness）**会显著下降——也就是说，**可能更难选出领导者或更慢达成共识**。

首先是节点存在拒绝投票的情况（同为candidate），所以必须确认投票后才能重置。

另外是要辨别过期旧心跳：

我们考虑下面一种情况，假设**初始**

- A 是 leader（term 1）。
- B、C 是 follower（term 1）。
- 网络分区：A ↔ B 通信被阻断，但 A ↔ C 正常，B ↔ C 正常。

B 收不到 A 的心跳。 因此 B 的选举计时器到期，B 升级为 candidate（term=2），向所有节点发送 `RequestVote(term=2)`。但此时，A 因为不知 B 已经超时，仍然会周期性向 C 发送心跳（term=1）。

C 收到 B 的 `RequestVote(term=2)`，按 Raft 规则会将自己 term 更新到 2，并投票给 B，重置自己的计时器。加入这个C到B的确认缺失，B由于拿不到大多数选票会继续等待重试；而C由于一直收到A的心跳也无法成为candidate。也就是说，潜在的候选人会因为假心跳被安抚，导致无法迅速的选出胜任的候选人



另一个很容易导致错误的点是，要把3B的内容带入到3A中思考。许多同学误以为“心跳”是一种**特殊的 RPC**，认为当一个节点接收到心跳时，它应该“与普通的 AppendEntries 不同地处理”。大家可能会这么处理：当收到一个心跳 RPC 时，**就立即重置选举计时器，然后直接返回 success（成功）**， 而**不执行 Figure 2 规定的那些检查步骤**。

这种做法是**极其危险的**。因为：当 follower 接受了这个 RPC 并返回 success 时， 它实际上是在向领导者**隐式地声明**：

> “我的日志和你（领导者）的日志在 `prevLogIndex` 这个位置之前都是匹配的。”

而领导者在收到这种（错误的）回复后，可能就会**错误地认为**某些日志条目已经被大多数节点复制，进而**错误地提交这些日志条目**。

#### 关于锁

**不要！不要！不要试图使用细粒度的锁**，有的同学可能认为只需要在设计共享变量的修改和查看时需要加锁，然后写出下面的代码：

```go
rf.mu.Lock()
rf.currentTerm += 1
rf.state = Candidate
for <each peer> {
  go func() {
    rf.mu.Lock()
    args.Term = rf.currentTerm
    rf.mu.Unlock()
    Call("Raft.RequestVote", &args, ...)
    // handle the reply...
  }()
}
rf.mu.Unlock()
```

这种写法的问题在于`args.Term` **并不一定等于** 外层代码变成 Candidate 时的 `rf.currentTerm`。 从 goroutine 创建到它实际读取 `rf.currentTerm` 之间可能过了很久，期间任期可能变了、节点状态可能已经不是 Candidate 了。


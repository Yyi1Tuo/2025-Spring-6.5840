package mr

import (
	"log"
	"net"
	"net/http"
	"net/rpc"
	"os"
	"sync"
	"time"
)

const (
	WaitingTask = 0
	MapPhase    = 1
	ReducePhase = 2
	DoneTask    = 3
)

type Task struct {
	Taskid    int
	Filename  string
	TaskType  int
	ReduceNum int
}

type Coordinator struct {
	// Your definitions here.
	TaskMapState      map[int]int       //监控任务的执行情况
	TaskAliveDetector map[int]time.Time //监控任务的存活情况
	Tasks             map[int]*Task

	TaskChan chan *Task
	lenfiles int
	Phase    int
}

var HeartbeatLock sync.Mutex
var TaskLock sync.Mutex
var PhaseLock sync.Mutex

// Your code here -- RPC handlers for the worker to call.

// an example RPC handler.
//
// the RPC argument and reply types are defined in rpc.go.
func (c *Coordinator) Example(args *ExampleArgs, reply *ExampleReply) error {
	reply.Y = args.X + 1
	return nil
}

func (c *Coordinator) AllocateTask(args *AllocateTaskArgs, reply *AllocateTaskReply) error {
	//这里需要设置一个超时器，如果暂时没有任务，返回waiting
	select {
	case task := <-c.TaskChan:
		HeartbeatLock.Lock()
		c.TaskAliveDetector[task.Taskid] = time.Now()
		defer HeartbeatLock.Unlock()
		reply.Task = task
		reply.Lenfiles = c.lenfiles
		return nil
	case <-time.After(800 * time.Millisecond):
		PhaseLock.Lock()
		defer PhaseLock.Unlock()
		if c.Phase == DoneTask {
			reply.Task = &Task{TaskType: DoneTask}
			reply.Lenfiles = 0
			return nil
		}
		reply.Task = &Task{TaskType: WaitingTask}
		reply.Lenfiles = 0
		return nil
	}
}
func (c *Coordinator) MarkTaskDone(args *ReportTaskDoneArgs, reply *ReportTaskDoneReply) error {
	TaskLock.Lock()
	HeartbeatLock.Lock()
	//fmt.Println("[DEBUG]Coordinator: Mark Task Done:", args.Taskid)
	defer HeartbeatLock.Unlock()
	defer TaskLock.Unlock()
	//由于rpc的gob会重新创建对象，所以不能直接传指针
	delete(c.TaskAliveDetector, args.Taskid)
	delete(c.Tasks, args.Taskid)
	delete(c.TaskMapState, args.Taskid)
	//c.TaskMapState[args.Taskid] = DoneTask
	//fmt.Println("MarkTaskDone", args.Taskid, c.TaskMapState[args.Taskid])
	return nil
}

// start a thread that listens for RPCs from worker.go
func (c *Coordinator) server() {
	rpc.Register(c)
	rpc.HandleHTTP()
	//l, e := net.Listen("tcp", ":1234")
	sockname := coordinatorSock()
	os.Remove(sockname)
	l, e := net.Listen("unix", sockname)
	if e != nil {
		log.Fatal("listen error:", e)
	}
	go http.Serve(l, nil)
}

// main/mrcoordinator.go calls Done() periodically to find out
// if the entire job has finished.
func (c *Coordinator) Done() bool {
	ret := false

	// Your code here.
	PhaseLock.Lock()
	defer PhaseLock.Unlock()
	ret = c.Phase == DoneTask

	return ret
}

// create a Coordinator.
// main/mrcoordinator.go calls this function.
// nReduce is the number of reduce tasks to use.
func MakeCoordinator(files []string, nReduce int) *Coordinator {
	c := Coordinator{
		TaskMapState:      make(map[int]int),
		TaskChan:          make(chan *Task, 2*len(files)),
		TaskAliveDetector: make(map[int]time.Time),
		Tasks:             make(map[int]*Task),
		lenfiles:          len(files),
		Phase:             MapPhase,
	}
	c.server()
	// Your code here.
	for i, v := range files {
		task := c.MakeMapTask(v, i, nReduce)
		c.TaskMapState[task.Taskid] = MapPhase
		c.Tasks[task.Taskid] = task
		c.TaskChan <- task
	}
	go monitor(&c)
	//需要某种机制来转换阶段，即从map->reduce
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			flag := true
			TaskLock.Lock()
			for _, state := range c.TaskMapState {
				if state == MapPhase {
					//fmt.Println("Map阶段未结束", taskid, state)
					flag = false
					break
				}
			}
			TaskLock.Unlock()
			if flag {
				//Map阶段结束了
				break
			}
			time.Sleep(100 * time.Millisecond)
		}

	}()

	wg.Wait()
	//fmt.Println("[DEBUG]Map阶段结束")
	//进入Reduce阶段
	PhaseLock.Lock()
	c.Phase = ReducePhase
	PhaseLock.Unlock()
	TaskLock.Lock()
	for i := 0; i < nReduce; i++ {
		task := c.MakeReduceTask(i, nReduce)
		c.TaskMapState[task.Taskid] = ReducePhase
		c.Tasks[task.Taskid] = task
		c.TaskChan <- task
	}
	TaskLock.Unlock()
	//判断reduce是否结束
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			flag := true
			TaskLock.Lock()
			for _, state := range c.TaskMapState {
				if state == ReducePhase {
					//fmt.Println("Reduce阶段未结束", taskid, state)
					flag = false
					break
				}
			}
			TaskLock.Unlock()
			if flag {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}

	}()
	wg.Wait()
	//fmt.Println("[DEBUG]Reduce阶段结束")
	//任务完成退出master
	PhaseLock.Lock()
	c.Phase = DoneTask
	PhaseLock.Unlock()
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
}

func (c *Coordinator) MakeMapTask(s string, i int, nReduce int) *Task {
	task := &Task{
		Taskid:    i,
		Filename:  s,
		TaskType:  MapPhase,
		ReduceNum: nReduce,
	}
	return task
}
func (c *Coordinator) MakeReduceTask(i int, nReduce int) *Task {
	task := &Task{
		Taskid:    i,
		TaskType:  ReducePhase,
		ReduceNum: nReduce,
		Filename:  "",
	}
	return task
}

func monitor(c *Coordinator) {
	//检测各Task运行状况
	ticker := time.NewTicker(3 * time.Second)
	for range ticker.C {
		HeartbeatLock.Lock()
		for taskid, lastHeartbeat := range c.TaskAliveDetector {
			TaskLock.Lock()
			if time.Since(lastHeartbeat) > 10*time.Second {
				newtask := c.Tasks[taskid]
				c.TaskChan <- newtask
				c.TaskAliveDetector[taskid] = time.Now()

			}
			TaskLock.Unlock()
		}
		HeartbeatLock.Unlock()
	}
}

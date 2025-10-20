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
	TaskMapState map[int]int //监控任务的执行情况
	TaskChan     chan *Task
	lenfiles     int
	Phase        int
}

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
	task := <-c.TaskChan
	reply.Task = task
	reply.Lenfiles = c.lenfiles
	//fmt.Println("[DEBUG]ALLOCATE TASK:", task.TaskType, task.Taskid)
	return nil
}
func (c *Coordinator) MarkTaskDone(args *ReportTaskDoneArgs, reply *ReportTaskDoneReply) error {
	TaskLock.Lock()
	defer TaskLock.Unlock()
	//由于rpc的gob会重新创建对象，所以不能直接传指针

	c.TaskMapState[args.Taskid] = DoneTask
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
		TaskMapState: make(map[int]int),
		TaskChan:     make(chan *Task, len(files)),
		lenfiles:     len(files),
		Phase:        MapPhase,
	}
	c.server()
	// Your code here.
	for i, v := range files {
		task := c.MakeMapTask(v, i, nReduce)
		c.TaskMapState[task.Taskid] = MapPhase
		c.TaskChan <- task
	}

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
			time.Sleep(1 * time.Second)
		}

	}()

	wg.Wait()

	//进入Reduce阶段
	PhaseLock.Lock()
	c.Phase = ReducePhase
	PhaseLock.Unlock()
	//fmt.Println("进入Reduce阶段")
	for i := 0; i < nReduce; i++ {
		task := c.MakeReduceTask(i, nReduce)
		c.TaskMapState[task.Taskid] = ReducePhase
		c.TaskChan <- task
	}

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
			time.Sleep(1 * time.Second)
		}

	}()
	wg.Wait()

	//任务完成退出master
	PhaseLock.Lock()
	c.Phase = DoneTask
	PhaseLock.Unlock()
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

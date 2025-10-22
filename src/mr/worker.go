package mr

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io/ioutil"
	"log"
	"net/rpc"
	"os"
	"sort"
	"strconv"
	"time"
)

// Map functions return a slice of KeyValue.
type KeyValue struct {
	Key   string
	Value string
}

// for sorting by key.
type ByKey []KeyValue

// for sorting by key.
func (a ByKey) Len() int           { return len(a) }
func (a ByKey) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a ByKey) Less(i, j int) bool { return a[i].Key < a[j].Key }

// use ihash(key) % NReduce to choose the reduce
// task number for each KeyValue emitted by Map.
func ihash(key string) int {
	h := fnv.New32a()
	h.Write([]byte(key))
	return int(h.Sum32() & 0x7fffffff)
}

// main/mrworker.go calls this function.
func Worker(mapf func(string, string) []KeyValue, reducef func(string, []string) string) {

	// Your worker implementation here.
	for {
		//fmt.Println("[DEBUG]Worker: Main Loop At", time.Now().Format("2006-01-02 15:04:05"))
		task, lenfiles := getTask()
		switch task.TaskType {
		case MapPhase:
			doMapTask(task, mapf)
		case ReducePhase:
			doReduceTask(task, reducef, lenfiles)
		case WaitingTask:
			time.Sleep(500 * time.Millisecond)
		case DoneTask:
			//fmt.Println("[DEBUG]Worker退出")
			return
		}
		time.Sleep(200 * time.Millisecond)
	}

}

// send an RPC request to the coordinator, wait for the response.
// usually returns true.
// returns false if something goes wrong.
func call(rpcname string, args interface{}, reply interface{}) bool {
	// c, err := rpc.DialHTTP("tcp", "127.0.0.1"+":1234")
	sockname := coordinatorSock()
	c, err := rpc.DialHTTP("unix", sockname)
	if err != nil {
		log.Fatal("dialing:", err)
	}
	defer c.Close()

	err = c.Call(rpcname, args, reply)
	if err == nil {
		return true
	}

	fmt.Println(err)
	return false
}

func getTask() (*Task, int) {
	args := AllocateTaskArgs{}
	replys := AllocateTaskReply{}
	//fmt.Println("[DEBUG]Worker: Get Task At", time.Now().Format("2006-01-02 15:04:05"))
	ok := call("Coordinator.AllocateTask", &args, &replys)
	if ok {
		//fmt.Println("[DEBUG]GET TASK:", replys.Task.TaskType, replys.Task.Taskid)
	} else {
		//假设没任务了
		return &Task{TaskType: DoneTask}, 0
	}
	return replys.Task, replys.Lenfiles
}

// 进行map任务
func doMapTask(Task *Task, mapf func(string, string) []KeyValue) {

	filename := Task.Filename
	file, err := os.Open(filename)
	if err != nil {
		log.Fatalf("cannot open %v", filename)
	}
	content, err := ioutil.ReadAll(file)
	if err != nil {
		log.Fatalf("cannot read %v", filename)
	}
	file.Close()
	intermediate := mapf(filename, string(content))

	HashKeyID := make([][]KeyValue, Task.ReduceNum) //代表这个键值对应该交付给哪个ReduceWorker
	for _, kv := range intermediate {
		HashKeyID[ihash(kv.Key)%Task.ReduceNum] = append(HashKeyID[ihash(kv.Key)%Task.ReduceNum], kv)
	}
	for i := 0; i < Task.ReduceNum; i++ {
		outputFilename := "mr-" + strconv.Itoa(Task.Taskid) + "-" + strconv.Itoa(i)
		ofile, err := os.Create(outputFilename)
		if err != nil {
			log.Fatalf("cannot create %v", outputFilename)
		}
		enc := json.NewEncoder(ofile)
		for _, kv := range HashKeyID[i] {
			err := enc.Encode(kv)
			if err != nil {
				log.Fatalf("cannot encode %v", kv)
			}
		}
		ofile.Close()
	}
	reportTaskDone(Task)
}

func doReduceTask(Task *Task, reducef func(string, []string) string, lenfiles int) {
	intermediate := []KeyValue{}
	for i := 0; i < lenfiles; i++ {
		inputFilename := "mr-" + strconv.Itoa(i) + "-" + strconv.Itoa(Task.Taskid)
		ofile, err := os.Open(inputFilename)
		if err != nil {
			log.Fatalf("cannot create %v", inputFilename)
		}
		dec := json.NewDecoder(ofile)
		for {
			var kv KeyValue
			if err := dec.Decode(&kv); err != nil {
				break
			}
			intermediate = append(intermediate, kv)
		}
		ofile.Close()
	}
	sort.Sort(ByKey(intermediate))

	//调用reducef函数
	outputFilename := "mr-out-" + strconv.Itoa(Task.Taskid)
	ofile, _ := os.Create(outputFilename)

	i := 0
	for i < len(intermediate) {
		j := i + 1
		for j < len(intermediate) && intermediate[j].Key == intermediate[i].Key {
			j++
		}
		values := []string{}
		for k := i; k < j; k++ {
			values = append(values, intermediate[k].Value)
		}
		output := reducef(intermediate[i].Key, values)

		// this is the correct format for each line of Reduce output.
		fmt.Fprintf(ofile, "%v %v\n", intermediate[i].Key, output)

		i = j
	}

	ofile.Close()
	reportTaskDone(Task)
}

func reportTaskDone(Task *Task) {
	args := ReportTaskDoneArgs{Taskid: Task.Taskid}
	replys := ReportTaskDoneReply{}
	//fmt.Println("[DEBUG]Worker: Report Task Done:", Task.Taskid)
	ok := call("Coordinator.MarkTaskDone", &args, &replys)
	if !ok {
		fmt.Printf("Report Task Done failed!\n")
	}
}

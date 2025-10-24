package kvsrv

import (
	"log"
	"sync"

	"6.5840/kvsrv1/rpc"
	"6.5840/labrpc"
	tester "6.5840/tester1"
)

const Debug = false

func DPrintf(format string, a ...interface{}) (n int, err error) {
	if Debug {
		log.Printf(format, a...)
	}
	return
}

type KVServer struct {
	mu sync.Mutex

	// Your definitions here.
	KV         map[string]string
	VersionMap map[string]rpc.Tversion
}

func MakeKVServer() *KVServer {
	kv := &KVServer{
		KV:         make(map[string]string),
		VersionMap: make(map[string]rpc.Tversion),
	}
	// Your code here.
	return kv
}

// Get returns the value and version for args.Key, if args.Key
// exists. Otherwise, Get returns ErrNoKey.
func (kv *KVServer) Get(args *rpc.GetArgs, reply *rpc.GetReply) {
	// Your code here.
	kv.mu.Lock()
	defer kv.mu.Unlock()
	value, ok := kv.KV[args.Key]
	if !ok {
		reply.Err = rpc.ErrNoKey
		return
	}
	reply.Err = rpc.OK
	reply.Value = value
	reply.Version = kv.VersionMap[args.Key]
	return
}

// Update the value for a key if args.Version matches the version of
// the key on the server. If versions don't match, return ErrVersion.
// If the key doesn't exist, Put installs the value if the
// args.Version is 0, and returns ErrNoKey otherwise.
func (kv *KVServer) Put(args *rpc.PutArgs, reply *rpc.PutReply) {
	// Your code here.
	key := args.Key
	value := args.Value
	version := args.Version
	kv.mu.Lock()
	defer kv.mu.Unlock()
	server_version, ok := kv.VersionMap[key]
	if !ok {
		if version == 0 {
			kv.KV[key] = value
			kv.VersionMap[key] = version + 1
			reply.Err = rpc.OK
			return
		} else {
			reply.Err = rpc.ErrNoKey
			return
		}
	}
	if version == server_version {
		kv.KV[key] = value
		kv.VersionMap[key] = version + 1
		reply.Err = rpc.OK
		return
	}
	reply.Err = rpc.ErrVersion
	return
}

// You can ignore Kill() for this lab
func (kv *KVServer) Kill() {
}

// You can ignore all arguments; they are for replicated KVservers
func StartKVServer(ends []*labrpc.ClientEnd, gid tester.Tgid, srv int, persister *tester.Persister) []tester.IService {
	kv := MakeKVServer()
	return []tester.IService{kv}
}

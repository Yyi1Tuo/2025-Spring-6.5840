package kvsrv

import (
	"time"

	"6.5840/kvsrv1/rpc"
	kvtest "6.5840/kvtest1"
	tester "6.5840/tester1"
)

type Clerk struct {
	clnt   *tester.Clnt
	server string
}

func MakeClerk(clnt *tester.Clnt, server string) kvtest.IKVClerk {
	ck := &Clerk{clnt: clnt, server: server}
	// You may add code here.
	return ck
}

// Get fetches the current value and version for a key.  It returns
// ErrNoKey if the key does not exist. It keeps trying forever in the
// face of all other errors.
//
// You can send an RPC with code like this:
// ok := ck.clnt.Call(ck.server, "KVServer.Get", &args, &reply)
//
// The types of args and reply (including whether they are pointers)
// must match the declared types of the RPC handler function's
// arguments. Additionally, reply must be passed as a pointer.
func (ck *Clerk) Get(key string) (string, rpc.Tversion, rpc.Err) {
	// You will have to modify this function.
	args := rpc.GetArgs{Key: key}
	reply := rpc.GetReply{}

	//ok是一个bool值，反映的是connection是否成功
	//只有ok为true，reply才有意义
	for !ck.clnt.Call(ck.server, "KVServer.Get", &args, &reply) {
		time.Sleep(100 * time.Millisecond)
	}
	DPrintf("[DEBUG]Client Get Reply : %+v", reply)
	//还有一个问题是Get操作需不需要考虑version？
	//在一个linearizable语意的 KV Server中，有一个明确的时间线，所以Get操作一定是当前时刻的最新值
	//也就是说，Get操作不需要考虑version，不会影响Get操作的正确性，只需要记录下version，返回给上层逻辑即可。
	if reply.Err == rpc.ErrNoKey {
		return "", 0, rpc.ErrNoKey
	}
	return reply.Value, reply.Version, reply.Err
}

// Put updates key with value only if the version in the
// request matches the version of the key at the server.  If the
// versions numbers don't match, the server should return
// ErrVersion.  If Put receives an ErrVersion on its first RPC, Put
// should return ErrVersion, since the Put was definitely not
// performed at the server. If the server returns ErrVersion on a
// resend RPC, then Put must return ErrMaybe to the application, since
// its earlier RPC might have been processed by the server successfully
// but the response was lost, and the Clerk doesn't know if
// the Put was performed or not.
//
// You can send an RPC with code like this:
// ok := ck.clnt.Call(ck.server, "KVServer.Put", &args, &reply)
//
// The types of args and reply (including whether they are pointers)
// must match the declared types of the RPC handler function's
// arguments. Additionally, reply must be passed as a pointer.
func (ck *Clerk) Put(key, value string, version rpc.Tversion) rpc.Err {
	// You will have to modify this function.
	args := rpc.PutArgs{Key: key, Value: value, Version: version}

	firstAttempt := true
	for {
		reply := rpc.PutReply{} //每次循环都要重新初始化reply，否则会产生复用的情况，导致gob不会把值覆盖掉
		ok := ck.clnt.Call(ck.server, "KVServer.Put", &args, &reply)
		if ok {
			DPrintf("[DEBUG]Client Put Reply : %+v", reply)
			if reply.Err == rpc.OK {
				return rpc.OK
			} else if reply.Err == rpc.ErrVersion {
				if firstAttempt {
					return rpc.ErrVersion
				} else {
					return rpc.ErrMaybe
				}
			}
			return reply.Err
		}
		firstAttempt = false
		time.Sleep(100 * time.Millisecond)
	}
}

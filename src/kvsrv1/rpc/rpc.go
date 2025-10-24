package rpc

type Err string

const (
	// Err's returned by server and Clerk
	OK         = "OK"
	ErrNoKey   = "ErrNoKey"
	ErrVersion = "ErrVersion"

	// Err returned by Clerk only
	ErrMaybe = "ErrMaybe"
	//ErrMaybe存在于这种情况：
	// 如果 Put 在第一次 RPC 调用中收到 ErrVersion，
	// 则 Put 必须直接返回 ErrVersion —— 因为此时可以确定
	// 服务器没有执行该 Put 操作。

	// 如果 Put 在重传的 RPC 调用中收到 ErrVersion，
	// 则必须返回 ErrMaybe —— 因为之前的 RPC 调用
	// 可能已经被服务器成功执行，但返回结果在网络中丢失，
	// 客户端无法确定 Put 是否真的被执行

	// For future kvraft lab
	ErrWrongLeader = "ErrWrongLeader"
	ErrWrongGroup  = "ErrWrongGroup"
)

type Tversion uint64

type PutArgs struct {
	Key     string
	Value   string
	Version Tversion
}

type PutReply struct {
	Err Err
}

type GetArgs struct {
	Key string
}

type GetReply struct {
	Value   string
	Version Tversion
	Err     Err
}

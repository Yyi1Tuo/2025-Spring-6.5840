package lock

import (
	"math/rand"
	"strconv"
	"time"

	"6.5840/kvsrv1/rpc"
	kvtest "6.5840/kvtest1"
)

type Lock struct {
	// IKVClerk is a go interface for k/v clerks: the interface hides
	// the specific Clerk type of ck but promises that ck supports
	// Put and Get.  The tester passes the clerk in when calling
	// MakeLock().
	ck kvtest.IKVClerk
	// You may add code here
	lockName string //锁的唯一标识
	clientID string //由于clerk没有暴露clientID，我们便在创建lock实例的时候生成一个id

}

func nrand() int64 {
	return time.Now().UnixNano() + int64(rand.Intn(1<<30))
}

// The tester calls MakeLock() and passes in a k/v clerk; your code can
// perform a Put or Get by calling lk.ck.Put() or lk.ck.Get().
//
// Use l as the key to store the "lock state" (you would have to decide
// precisely what the lock state is).
func MakeLock(ck kvtest.IKVClerk, l string) *Lock {
	lk := &Lock{ck: ck, lockName: l, clientID: strconv.FormatInt(nrand(), 10)}
	// You may add code here
	return lk
}

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

func (lk *Lock) Release() {
	// Your code here
	for {
		locker, ver, err := lk.ck.Get(lk.lockName)
		if err == rpc.ErrNoKey {
			//没有锁，直接返回
			return
		}
		if locker != lk.clientID {
			//不是自己持有锁，直接返回
			return
		}
		lockErr := lk.ck.Put(lk.lockName, "", ver)
		switch lockErr {
		case rpc.OK:
			return
		default:
		}
		//release的优先级更高，无需退避，失败直接重试
	}
}

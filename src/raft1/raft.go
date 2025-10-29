package raft

// The file raftapi/raft.go defines the interface that raft must
// expose to servers (or the tester), but see comments below for each
// of these functions for more details.
//
// Make() creates a new raft peer that implements the raft interface.

import (
	//	"bytes"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	//	"6.5840/labgob"
	"6.5840/labrpc"
	"6.5840/raftapi"
	tester "6.5840/tester1"
)

const (
	Leader    = 2
	Candidate = 1
	Follower  = 0
)

type logEntry struct {
	Term    int
	Command interface{}
}

// A Go object implementing a single Raft peer.
type Raft struct {
	mu        sync.Mutex          // Lock to protect shared access to this peer's state
	peers     []*labrpc.ClientEnd // RPC end points of all peers
	persister *tester.Persister   // Object to hold this peer's persisted state
	me        int                 // this peer's index into peers[]
	dead      int32               // set by Kill()

	// Your data here (3A, 3B, 3C).
	// Look at the paper's Figure 2 for a description of what
	// state a Raft server must maintain.
	currTerm    int
	voteFor     int
	log         []logEntry
	commitIndex int //已提交日志索引
	lastApplied int //已应用日志的索引

	state          int
	electionTimer  *time.Timer
	heartbeatTimer *time.Timer
	voteCount      int
}

// return currentTerm and whether this server
// believes it is the leader.
func (rf *Raft) GetState() (int, bool) {

	var term int
	var isleader bool
	// Your code here (3A).
	rf.mu.Lock()
	defer rf.mu.Unlock()
	term = rf.currTerm
	isleader = rf.state == Leader
	return term, isleader
}

// save Raft's persistent state to stable storage,
// where it can later be retrieved after a crash and restart.
// see paper's Figure 2 for a description of what should be persistent.
// before you've implemented snapshots, you should pass nil as the
// second argument to persister.Save().
// after you've implemented snapshots, pass the current snapshot
// (or nil if there's not yet a snapshot).
func (rf *Raft) persist() {
	// Your code here (3C).
	// Example:
	// w := new(bytes.Buffer)
	// e := labgob.NewEncoder(w)
	// e.Encode(rf.xxx)
	// e.Encode(rf.yyy)
	// raftstate := w.Bytes()
	// rf.persister.Save(raftstate, nil)
}

// restore previously persisted state.
func (rf *Raft) readPersist(data []byte) {
	if data == nil || len(data) < 1 { // bootstrap without any state?
		return
	}
	// Your code here (3C).
	// Example:
	// r := bytes.NewBuffer(data)
	// d := labgob.NewDecoder(r)
	// var xxx
	// var yyy
	// if d.Decode(&xxx) != nil ||
	//    d.Decode(&yyy) != nil {
	//   error...
	// } else {
	//   rf.xxx = xxx
	//   rf.yyy = yyy
	// }
}

// how many bytes in Raft's persisted log?
func (rf *Raft) PersistBytes() int {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	return rf.persister.RaftStateSize()
}

// 当上层服务创建了一个包含至 index（含 index）为止所有信息的快照时，
// 表示服务层不再需要该索引及其之前的日志。
// 此时，Raft 应当尽可能地截断（trim）日志。
func (rf *Raft) Snapshot(index int, snapshot []byte) {
	// Your code here (3D).

}

type RequestVoteArgs struct {
	// Your data here (3A, 3B).
	Term        int
	CandidateId int
}
type RequestVoteReply struct {
	// Your data here (3A).
	Term        int
	VoteGranted bool
}
type AppendEntriesArgs struct {
	Term         int
	LeaderId     int
	PrevLogIndex int
	PrevLogTerm  int
	Entries      []logEntry
	LeaderCommit int
}
type AppendEntriesReply struct {
	Term    int
	Success bool
}

// example RequestVote RPC handler.
func (rf *Raft) RequestVote(args *RequestVoteArgs, reply *RequestVoteReply) {
	// Your code here (3A, 3B).
	//这里应当仔细阅读RAFT原文对于投票规则的介绍
	// If votedFor is null or candidateId, and candidate’s log is at
	// least as up-to-date as receiver’s log, grant vote
	term := args.Term
	DPrintf("Server %d receive RequestVote from %d, Term: %d", rf.me, args.CandidateId, term)
	reply.VoteGranted = false
	rf.mu.Lock()
	defer rf.mu.Unlock()
	if term < rf.currTerm {
		reply.Term = rf.currTerm
		return
	}
	if term > rf.currTerm {
		//收到更大任期的投票请求，无论此时状态如何，立刻转为Follower，并更新任期
		rf.currTerm = term
		rf.state = Follower
		rf.voteFor = -1
		rf.voteCount = 0
		//rf.timer.Reset(getRandomElectionTimeout())只有确认投票才重置超时器
	}
	if term == rf.currTerm {
		reply.Term = rf.currTerm
		if rf.voteFor == -1 || rf.voteFor == args.CandidateId {
			//3A暂不考虑
			//`if len(rf.log) == 0 || args.LastLogIndex >= len(rf.log)-1 {
			reply.VoteGranted = true
			rf.voteFor = args.CandidateId
			DPrintf("Server %d vote for %d, Term: %d", rf.me, args.CandidateId, rf.currTerm)
			rf.electionTimer.Reset(getRandomElectionTimeout())
			return
		}
	}
}

func (rf *Raft) sendRequestVote(server int, args *RequestVoteArgs, reply *RequestVoteReply) bool {
	if ok := rf.peers[server].Call("Raft.RequestVote", args, reply); !ok {
		return false
	}
	rf.mu.Lock()
	defer rf.mu.Unlock()
	if reply.VoteGranted {
		rf.voteCount++
		if rf.state == Follower {
			return true
		}
		if rf.state == Candidate && rf.voteCount > len(rf.peers)/2 {
			//统计投票数
			DPrintf("Server %d becomeLeader, Term: %d", rf.me, rf.currTerm)
			rf.becomeLeader()
			return true
		} else if rf.state == Candidate {
			DPrintf("Server %d cannot get enough votes, Term: %d, VoteCount: %d", rf.me, rf.currTerm, rf.voteCount)
		}
	}
	return true
}
func (rf *Raft) sendAppendEntries(server int, args *AppendEntriesArgs, reply *AppendEntriesReply) {
	if ok := rf.peers[server].Call("Raft.AppendEntries", args, reply); !ok {
		return
	}
	rf.mu.Lock()
	defer rf.mu.Unlock()
	if !reply.Success {
		//false有多种可能，在3A我们只考虑任期不一致的情况
		if reply.Term > rf.currTerm {
			rf.currTerm = reply.Term
			rf.state = Follower
			rf.voteCount = 0
			rf.voteFor = -1
			rf.electionTimer.Reset(getRandomElectionTimeout())
		}
	}
}
func (rf *Raft) AppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply) {
	term := args.Term
	DPrintf("Server %d receive AppendEntries from %d, Term: %d", rf.me, args.LeaderId, term)
	rf.mu.Lock()
	defer rf.mu.Unlock()
	if term < rf.currTerm {
		reply.Term = rf.currTerm
		reply.Success = false
		return
	}
	//true if follower contained entry matching prevLogIndex and prevLogTerm
	//但是3A暂不考虑
	if term >= rf.currTerm {
		//原论文中没有提到server收到相同任期的心跳要不要转为follower
		//但是我们思考一下，如果一个candidate收到心跳，意味着此时已经选出了leader
		//那么这个candidate应当立即转为follower，并更新任期
		rf.currTerm = term
		rf.state = Follower
		rf.voteCount = 0
		//TODO:这里应当考虑是否需要重置超时器，在3A中可以认为直接更新
		rf.electionTimer.Reset(getRandomElectionTimeout())
		reply.Term = rf.currTerm
		reply.Success = true
		return
	}
}

// the service using Raft (e.g. a k/v server) wants to start
// agreement on the next command to be appended to Raft's log. if this
// server isn't the leader, returns false. otherwise start the
// agreement and return immediately. there is no guarantee that this
// command will ever be committed to the Raft log, since the leader
// may fail or lose an election. even if the Raft instance has been killed,
// this function should return gracefully.
//
// the first return value is the index that the command will appear at
// if it's ever committed. the second return value is the current
// term. the third return value is true if this server believes it is
// the leader.
func (rf *Raft) Start(command interface{}) (int, int, bool) {
	index := -1
	term := -1
	isLeader := true

	// Your code here (3B).

	return index, term, isLeader
}

// the tester doesn't halt goroutines created by Raft after each test,
// but it does call the Kill() method. your code can use killed() to
// check whether Kill() has been called. the use of atomic avoids the
// need for a lock.
//
// the issue is that long-running goroutines use memory and may chew
// up CPU time, perhaps causing later tests to fail and generating
// confusing debug output. any goroutine with a long-running loop
// should call killed() to check whether it should stop.
func (rf *Raft) Kill() {
	atomic.StoreInt32(&rf.dead, 1)
	// Your code here, if desired.
}

func (rf *Raft) killed() bool {
	z := atomic.LoadInt32(&rf.dead)
	return z == 1
}

func (rf *Raft) ticker() {
	for rf.killed() == false {
		// Your code here (3A)
		// Check if a leader election should be started.
		select {
		case <-rf.electionTimer.C:
			rf.mu.Lock()
			if rf.state != Leader {
				DPrintf("Server %d Election Timeout, start election", rf.me)
				rf.startElection()
				rf.electionTimer.Reset(getRandomElectionTimeout())
			}
			rf.mu.Unlock()
		case <-rf.heartbeatTimer.C:
			rf.mu.Lock()
			if rf.state == Leader {
				rf.broadcastHeartbeat()
				rf.heartbeatTimer.Reset(100 * time.Millisecond)
			}
			rf.mu.Unlock()
		}
		// pause for a random amount of time between 50 and 350
		// milliseconds.
		//time.Sleep(getRandomElectionTimeout())
	}
}
func (rf *Raft) startElection() {
	rf.currTerm++
	rf.voteFor = rf.me
	rf.voteCount = 1
	rf.state = Candidate
	rf.electionTimer.Reset(getRandomElectionTimeout())
	DPrintf("%d startElection%d", rf.me, rf.currTerm)

	for i := range rf.peers {
		if i == rf.me {
			continue
		}
		go func(i int) {
			rf.sendRequestVote(i, &RequestVoteArgs{Term: rf.currTerm, CandidateId: rf.me}, &RequestVoteReply{})
		}(i)
	}

	//DPrintf("Server %d ELectionEnd, Term: %d, VoteCount: %d", rf.me, rf.currTerm, rf.voteCount)
}
func (rf *Raft) broadcastHeartbeat() {
	for i := range rf.peers {
		if i == rf.me {
			continue
		}
		go func(i int) {
			rf.sendAppendEntries(i, &AppendEntriesArgs{Term: rf.currTerm, LeaderId: rf.me}, &AppendEntriesReply{})
		}(i)
	}
}
func (rf *Raft) becomeLeader() {
	rf.state = Leader
	rf.voteCount = 0
	rf.voteFor = -1
	go rf.broadcastHeartbeat()
	rf.heartbeatTimer.Reset(100 * time.Millisecond)
}

// the service or tester wants to create a Raft server. the ports
// of all the Raft servers (including this one) are in peers[]. this
// server's port is peers[me]. all the servers' peers[] arrays
// have the same order. persister is a place for this server to
// save its persistent state, and also initially holds the most
// recent saved state, if any. applyCh is a channel on which the
// tester or service expects Raft to send ApplyMsg messages.
// Make() must return quickly, so it should start goroutines
// for any long-running work.
func Make(peers []*labrpc.ClientEnd, me int,
	persister *tester.Persister, applyCh chan raftapi.ApplyMsg) raftapi.Raft {
	rf := &Raft{}
	rf.peers = peers
	rf.persister = persister
	rf.me = me

	// Your initialization code here (3A, 3B, 3C).
	rf.currTerm = 0
	rf.voteFor = -1
	rf.log = []logEntry{}
	rf.commitIndex = 0
	rf.lastApplied = 0
	rf.state = Follower
	rf.voteCount = 0
	rf.electionTimer = time.NewTimer(getRandomElectionTimeout())
	rf.heartbeatTimer = time.NewTimer(100 * time.Millisecond)
	// initialize from state persisted before a crash
	rf.readPersist(persister.ReadRaftState())

	// start ticker goroutine to start elections
	go rf.ticker()

	return rf
}
func getRandomElectionTimeout() time.Duration {
	return time.Duration(250+rand.Int63()%250) * time.Millisecond
}

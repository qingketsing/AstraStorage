// Raft部分
// 基于6.5840的raft

package raft

// The file raftapi/raft.go defines the interface that raft must
// expose to servers (or the tester), but see comments below for each
// of these functions for more details.
//
// Make() creates a new raft peer that implements the raft interface.

import (
	"bytes"
	"encoding/gob"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"
)

// ApplyMsg is sent to the application layer when a log entry is committed
type ApplyMsg struct {
	CommandValid bool
	Command      interface{}
	CommandIndex int

	// For snapshot
	SnapshotValid bool
	Snapshot      []byte
	SnapshotTerm  int
	SnapshotIndex int
}

// Peer represents a remote Raft node interface
type Peer interface {
	Call(method string, args interface{}, reply interface{}) bool
}

// Persister interface for saving state
type Persister interface {
	Save(raftstate []byte, snapshot []byte)
	ReadRaftState() []byte
	ReadSnapshot() []byte
	RaftStateSize() int
}

type LogEntry struct {
	Command interface{}
	Term    int
	Index   int
}

type PersistentState struct {
	CurrentTerm       int
	VotedFor          int
	Log               []LogEntry
	LastIncludedIndex int
	LastIncludedTerm  int
}

// A Go object implementing a single Raft peer.
type Raft struct {
	mu        sync.Mutex // Lock to protect shared access to this peer's state
	peers     []Peer     // RPC end points of all peers
	persister Persister  // Object to hold this peer's persisted state
	me        int        // this peer's index into peers[]
	dead      int32      // set by Kill()

	// Your data here (3A, 3B, 3C).
	// Look at the paper's Figure 2 for a description of what
	// state a Raft server must maintain.
	state           string
	currentTerm     int
	votedFor        int
	log             []LogEntry
	ElectionTimeout time.Duration

	// Volatile state for all server
	commitIndex int
	lastApplied int

	// Volatile state on leader
	nextIndex    []int
	matchedIndex []int

	// last time we recieved a heartbeat
	lastHeartbeat time.Time

	// snapshot part
	lastIncludedIndex int
	lastIncludeTerm   int

	// channel for apply
	applyCh chan ApplyMsg
}

type AppendEntryArgs struct {
	Term         int
	LeaderId     int
	PrevLogIndex int
	PrevLogTerm  int
	Entries      []LogEntry
	LeaderCommit int
}

type AppendEntryReply struct {
	Success       bool
	Term          int
	ConflictIndex int
	ConflictTerm  int
}

// example RequestVote RPC arguments structure.
// field names must start with capital letters!
type RequestVoteArgs struct {
	// Your data here (3A, 3B).
	Term         int
	CandidateId  int
	LastLogIndex int
	LastLogTerm  int
}

// example RequestVote RPC reply structure.
// field names must start with capital letters!
type RequestVoteReply struct {
	// Your data here (3A).
	Term        int
	VoteGranted bool
}

// Snapshot RPC args structure
// field names must start with capital letters!
type InstallSnapShotArgs struct {
	Term              int
	LeaderID          int
	LastIncludedIndex int
	LastIncludedTerm  int
	Data              []byte
}

type InstallSnapshotReply struct {
	Term int
}

// return currentTerm and whether this server
// believes it is the leader.
func (rf *Raft) GetState() (int, bool) {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	var term int
	var isleader bool
	// Your code here (3A).
	term = rf.currentTerm
	isleader = (rf.state == "Leader")

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
	w := new(bytes.Buffer)
	e := gob.NewEncoder(w)
	e.Encode(rf.currentTerm)
	e.Encode(rf.votedFor)
	e.Encode(rf.log)
	e.Encode(rf.lastIncludedIndex)
	e.Encode(rf.lastIncludeTerm)
	raftstate := w.Bytes()
	// 保持原有快照不变
	rf.persister.Save(raftstate, rf.persister.ReadSnapshot())
}

// 新增：同时持久化状态和快照
func (rf *Raft) persistStateAndSnapshot(snapshot []byte) {
	w := new(bytes.Buffer)
	e := gob.NewEncoder(w)
	e.Encode(rf.currentTerm)
	e.Encode(rf.votedFor)
	e.Encode(rf.log)
	e.Encode(rf.lastIncludedIndex)
	e.Encode(rf.lastIncludeTerm)
	raftstate := w.Bytes()
	rf.persister.Save(raftstate, snapshot)
}

// restore previously persisted state.
func (rf *Raft) readPersist(data []byte) {
	if len(data) < 1 { // bootstrap without any state?
		return
	}
	r := bytes.NewBuffer(data)
	d := gob.NewDecoder(r)
	var currentTerm int
	var votedFor int
	var log []LogEntry
	var lastIncludedIndex int
	var lastIncludedTerm int

	if d.Decode(&currentTerm) != nil ||
		d.Decode(&votedFor) != nil ||
		d.Decode(&log) != nil {
		// error
		DPrintf("[%d] readPersist decode error", rf.me)
		return
	}
	rf.currentTerm = currentTerm
	rf.votedFor = votedFor
	rf.log = log

	if d.Decode(&lastIncludedIndex) == nil && d.Decode(&lastIncludedTerm) == nil {
		rf.lastIncludedIndex = lastIncludedIndex
		rf.lastIncludeTerm = lastIncludedTerm
	}
	DPrintf("[%d] readPersist: term=%d, votedFor=%d, logLen=%d, lastIncludedIndex=%d",
		rf.me, rf.currentTerm, rf.votedFor, len(rf.log), rf.lastIncludedIndex)
}

// how many bytes in Raft's persisted log?
func (rf *Raft) PersistBytes() int {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	return rf.persister.RaftStateSize()
}

// the service says it has created a snapshot that has
// all info up to and including index. this means the
// service no longer needs the log through (and including)
// that index. Raft should now trim its log as much as possible.
func (rf *Raft) Snapshot(index int, snapshot []byte) {
	// Your code here (3D).
	rf.mu.Lock()
	defer rf.mu.Unlock()

	if index <= rf.lastIncludedIndex {
		return
	}

	if index > rf.commitIndex {
		return
	}

	logIdx := rf.logIndex(index)
	if logIdx < 0 || logIdx >= len(rf.log) {
		return
	}
	term := rf.log[logIdx].Term

	// 创建新日志：哨兵 + 之后的条目
	newLog := make([]LogEntry, 0)
	newLog = append(newLog, LogEntry{
		Term:  term,
		Index: index,
	})

	// 保留index之后的条目
	if logIdx+1 < len(rf.log) {
		newLog = append(newLog, rf.log[logIdx+1:]...)
	}

	rf.log = newLog
	rf.lastIncludeTerm = term
	rf.lastIncludedIndex = index

	rf.persistStateAndSnapshot(snapshot)

}

// example RequestVote RPC handler.
func (rf *Raft) RequestVote(args *RequestVoteArgs, reply *RequestVoteReply) {
	// Your code here (3A, 3B).
	rf.mu.Lock()
	defer rf.mu.Unlock()

	reply.Term = rf.currentTerm
	reply.VoteGranted = false

	// if leader's Term is less than the follower it will refuse voting
	if args.Term < rf.currentTerm {
		return
	}

	if args.Term > rf.currentTerm {
		// change the state of rf follower
		rf.currentTerm = args.Term
		rf.state = "Follower"
		rf.votedFor = -1
		rf.persist()
	}

	// Check if we can vote for this candidate
	if rf.votedFor == -1 || rf.votedFor == args.CandidateId {
		// Check if candidate's log is at least as up-to-date as ours
		lastLogIndex, lastLogTerm := rf.getLastLogInfo()

		logOk := args.LastLogTerm > lastLogTerm || (args.LastLogTerm == lastLogTerm && args.LastLogIndex >= lastLogIndex)

		if logOk {
			// granted votes
			rf.votedFor = args.CandidateId
			reply.VoteGranted = true
			rf.persist()

			// reset the lastheartbeat and election timeout
			rf.lastHeartbeat = time.Now()
			rf.ElectionTimeout = time.Duration(200+rand.Intn(301)) * time.Millisecond
		}
	}

	reply.Term = rf.currentTerm
}

// InstallSnapshot RPC handler
func (rf *Raft) InstallSnapshot(args *InstallSnapShotArgs, reply *InstallSnapshotReply) {
	rf.mu.Lock()
	DPrintf("[%d] InstallSnapshot: recv from %d, args.LastIncludedIndex=%d, args.LastIncludedTerm=%d, my lastIncludedIndex=%d",
		rf.me, args.LeaderID, args.LastIncludedIndex, args.LastIncludedTerm, rf.lastIncludedIndex)

	// Reply immediately if term < currentTerm
	if args.Term < rf.currentTerm {
		reply.Term = rf.currentTerm
		rf.mu.Unlock()
		DPrintf("[%d] InstallSnapshot: rejected due to term, args.Term=%d < currentTerm=%d", rf.me, args.Term, rf.currentTerm)
		return
	}

	// Update term and convert to follower if necessary
	if args.Term > rf.currentTerm {
		rf.currentTerm = args.Term
		rf.state = "Follower"
		rf.votedFor = -1
		rf.persist()
	}

	reply.Term = rf.currentTerm

	// Reset election timer
	rf.lastHeartbeat = time.Now()

	// Reply and ignore snapshot if it's older than our current snapshot
	if args.LastIncludedIndex <= rf.lastIncludedIndex {
		rf.mu.Unlock()
		DPrintf("[%d] InstallSnapshot: ignored, args.LastIncludedIndex=%d <= my lastIncludedIndex=%d",
			rf.me, args.LastIncludedIndex, rf.lastIncludedIndex)
		return
	}

	// 更新快照状态
	rf.lastIncludedIndex = args.LastIncludedIndex
	rf.lastIncludeTerm = args.LastIncludedTerm

	// 简单丢弃所有日志，leader会发送快照之后的条目
	rf.log = []LogEntry{{Term: args.LastIncludedTerm, Index: args.LastIncludedIndex}}
	DPrintf("[%d] InstallSnapshot: reset log to sentinel, lastIncludedIndex=%d", rf.me, args.LastIncludedIndex)

	// 更新commitIndex和lastApplied
	if rf.commitIndex < args.LastIncludedIndex {
		rf.commitIndex = args.LastIncludedIndex
	}
	if rf.lastApplied < args.LastIncludedIndex {
		rf.lastApplied = args.LastIncludedIndex
	}

	// 持久化状态和快照
	rf.persistStateAndSnapshot(args.Data)

	DPrintf("[%d] InstallSnapshot: updated state, lastIncludedIndex=%d, commitIndex=%d, lastApplied=%d, log len=%d",
		rf.me, rf.lastIncludedIndex, rf.commitIndex, rf.lastApplied, len(rf.log))

	rf.mu.Unlock()

	// 发送快照到应用层（使用goroutine避免阻塞RPC处理）
	go func() {
		DPrintf("[%d] InstallSnapshot: sending snapshot to applyCh, index=%d", rf.me, args.LastIncludedIndex)
		rf.applyCh <- ApplyMsg{
			SnapshotValid: true,
			Snapshot:      args.Data,
			SnapshotTerm:  args.LastIncludedTerm,
			SnapshotIndex: args.LastIncludedIndex,
		}
		DPrintf("[%d] InstallSnapshot: snapshot sent to applyCh", rf.me)
	}()
}

// example code to send a RequestVote RPC to a server.
// server is the index of the target server in rf.peers[].
// expects RPC arguments in args.
// fills in *reply with RPC reply, so caller should
// pass &reply.
// the types of the args and reply passed to Call() must be
// the same as the types of the arguments declared in the
// handler function (including whether they are pointers).
//
// The labrpc package simulates a lossy network, in which servers
// may be unreachable, and in which requests and replies may be lost.
// Call() sends a request and waits for a reply. If a reply arrives
// within a timeout interval, Call() returns true; otherwise
// Call() returns false. Thus Call() may not return for a while.
// A false return can be caused by a dead server, a live server that
// can't be reached, a lost request, or a lost reply.
//
// Call() is guaranteed to return (perhaps after a delay) *except* if the
// handler function on the server side does not return.  Thus there
// is no need to implement your own timeouts around Call().
//
// look at the comments in ../labrpc/labrpc.go for more details.
//
// if you're having trouble getting RPC to work, check that you've
// capitalized all field names in structs passed over RPC, and
// that the caller passes the address of the reply struct with &, not
// the struct itself.
func (rf *Raft) sendRequestVote(server int, args *RequestVoteArgs, reply *RequestVoteReply) bool {
	ok := rf.peers[server].Call("Raft.RequestVote", args, reply)
	return ok
}

func (rf *Raft) logIndex(index int) int {
	if rf.lastIncludedIndex == 0 {
		// 没有快照，日志从索引1开始，存储在log[0]
		return index - 1
	}
	// 有快照，log[0]是哨兵（lastIncludedIndex），log[1]是lastIncludedIndex+1
	return index - rf.lastIncludedIndex
}

// 获取最后一条日志的索引和任期
func (rf *Raft) getLastLogInfo() (int, int) {
	if len(rf.log) == 0 {
		return rf.lastIncludedIndex, rf.lastIncludeTerm
	}
	lastEntry := rf.log[len(rf.log)-1]
	return lastEntry.Index, lastEntry.Term
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
	rf.mu.Lock()
	defer rf.mu.Unlock()

	// check it is leader or not
	if rf.state != "Leader" {
		return -1, rf.currentTerm, false
	}

	lastIndex, _ := rf.getLastLogInfo()
	index := lastIndex + 1

	term := rf.currentTerm
	entry := LogEntry{
		Command: command,
		Term:    term,
		Index:   index,
	}
	rf.log = append(rf.log, entry)
	rf.persist()

	DPrintf("[%d] Start: new command at index=%d, term=%d, log len=%d, lastIncludedIndex=%d",
		rf.me, index, term, len(rf.log), rf.lastIncludedIndex)
	return index, term, true
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

func (rf *Raft) startElection() {
	// we assume that the state of which will start election is must be follower
	rf.mu.Lock()
	if rf.killed() {
		rf.mu.Unlock()
		return
	}
	rf.state = "Candidate"
	rf.currentTerm++
	term := rf.currentTerm
	rf.votedFor = rf.me
	rf.persist()
	me := rf.me
	electionTimeout := rf.ElectionTimeout

	lastIndex, lastTerm := rf.getLastLogInfo()

	rf.mu.Unlock()

	// gather vote request to all rf followers
	// and start count , if more than half of followers agree it became leader
	// then it will became leader and immediately send heartbeats to all followers
	var voteCount int32 = 1
	n := len(rf.peers)
	majority := n/2 + 1

	// If single node or already majority (e.g. 1 node cluster), become leader immediately
	if int(voteCount) >= majority {
		rf.state = "Leader"
		rf.lastHeartbeat = time.Now()
		lastIdx, _ := rf.getLastLogInfo()

		rf.nextIndex = make([]int, n)
		rf.matchedIndex = make([]int, n)
		for k := 0; k < n; k++ {
			rf.nextIndex[k] = lastIdx + 1
			rf.matchedIndex[k] = 0
		}
		// No peers to replicate to, but we are leader
		rf.mu.Unlock()
		return
	}

	arg := RequestVoteArgs{
		Term:         term,
		CandidateId:  me,
		LastLogIndex: lastIndex,
		LastLogTerm:  lastTerm,
	}
	type voteResult struct {
		ok    bool
		reply RequestVoteReply
	}
	ch := make(chan voteResult, n-1)

	for i := 0; i < n; i++ {
		if i == me {
			continue
		}
		server := i
		go func(server int) {
			var reply RequestVoteReply
			ok := rf.sendRequestVote(server, &arg, &reply)
			ch <- voteResult{ok: ok, reply: reply}
		}(server)
	}

	for replies := 0; replies < n-1; replies++ {
		select {
		case vr := <-ch:
			if !vr.ok {
				// RPC failed
				continue
			}
			reply := vr.reply

			if reply.Term > term {
				rf.mu.Lock()
				if reply.Term > rf.currentTerm {
					rf.currentTerm = reply.Term
					rf.state = "Follower"
					rf.votedFor = -1
					rf.persist()
					rf.lastHeartbeat = time.Now()
					rf.ElectionTimeout = time.Duration(200+rand.Intn(301)) * time.Millisecond
				}
				rf.mu.Unlock()
				return
			}

			if reply.Term == term && reply.VoteGranted {
				newCount := atomic.AddInt32(&voteCount, 1)
				if int(newCount) >= majority {
					rf.mu.Lock()
					// check the state again and make sure it is candidate
					if rf.state == "Candidate" && rf.currentTerm == term {
						rf.state = "Leader"
						rf.lastHeartbeat = time.Now()
						lastIdx, _ := rf.getLastLogInfo()

						rf.nextIndex = make([]int, n)
						rf.matchedIndex = make([]int, n)
						for k := 0; k < n; k++ {
							rf.nextIndex[k] = lastIdx + 1
							rf.matchedIndex[k] = 0
						}

						for k := 0; k < n; k++ {
							if k == rf.me {
								continue
							}
							go rf.replicateTo(k)
						}

						// rf.persist()
					}
					rf.mu.Unlock()
					return
				}
			}
		case <-time.After(electionTimeout):
			return
		}
	}

	deadline := time.Now().Add(electionTimeout + 150*time.Millisecond)
	for {
		if rf.killed() {
			return
		}
		rf.mu.Lock()
		if rf.state != "Candidate" || rf.currentTerm != term {
			rf.mu.Unlock()
			return
		}
		rf.mu.Unlock()

		// if timeout ,then cut it down and wait for retry
		if time.Now().After(deadline) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (rf *Raft) replicateTo(peer int) {
	for {
		if rf.killed() {
			return
		}

		rf.mu.Lock()
		if rf.state != "Leader" {
			rf.mu.Unlock()
			return
		}

		// snapshot needed fields
		next := rf.nextIndex[peer] // 1-based

		// Check if we need to send a snapshot
		if next <= rf.lastIncludedIndex {
			// Send InstallSnapshot RPC
			DPrintf("[%d] replicateTo %d: need snapshot, next=%d <= lastIncludedIndex=%d",
				rf.me, peer, next, rf.lastIncludedIndex)

			// Capture the snapshot info before releasing the lock
			snapshotIndex := rf.lastIncludedIndex
			snapshotTerm := rf.lastIncludeTerm

			args := InstallSnapShotArgs{
				Term:              rf.currentTerm,
				LeaderID:          rf.me,
				LastIncludedIndex: snapshotIndex,
				LastIncludedTerm:  snapshotTerm,
				Data:              rf.persister.ReadSnapshot(),
			}
			rf.mu.Unlock()

			var reply InstallSnapshotReply
			ok := rf.peers[peer].Call("Raft.InstallSnapshot", &args, &reply)
			if !ok {
				DPrintf("[%d] replicateTo %d: InstallSnapshot RPC failed", rf.me, peer)
				time.Sleep(15 * time.Millisecond)
				continue
			}
			DPrintf("[%d] replicateTo %d: InstallSnapshot RPC succeeded, reply.Term=%d", rf.me, peer, reply.Term)

			rf.mu.Lock()
			if reply.Term > rf.currentTerm {
				rf.currentTerm = reply.Term
				rf.state = "Follower"
				rf.votedFor = -1
				rf.persist()
				rf.mu.Unlock()
				return
			}

			// Update nextIndex and matchIndex based on the snapshot we sent
			// not the current lastIncludedIndex (which may have changed)
			rf.nextIndex[peer] = snapshotIndex + 1
			rf.matchedIndex[peer] = snapshotIndex
			DPrintf("[%d] replicateTo %d: updated nextIndex=%d, matchIndex=%d after snapshot",
				rf.me, peer, rf.nextIndex[peer], rf.matchedIndex[peer])
			rf.mu.Unlock()

			// Immediately continue to send any following entries
			continue
		}

		prevIndex := next - 1
		prevTerm := 0

		if prevIndex == rf.lastIncludedIndex {
			prevTerm = rf.lastIncludeTerm
		} else if prevIndex > rf.lastIncludedIndex {
			logIdx := rf.logIndex(prevIndex)
			if logIdx >= 0 && logIdx < len(rf.log) {
				prevTerm = rf.log[logIdx].Term
			}
		}

		entries := []LogEntry{}
		lastLogIndex, _ := rf.getLastLogInfo()

		if next <= lastLogIndex {
			startIdx := rf.logIndex(next)
			if startIdx >= 0 && startIdx < len(rf.log) {
				entries = append([]LogEntry(nil), rf.log[startIdx:]...)
			}
		}

		args := AppendEntryArgs{
			Term:         rf.currentTerm,
			LeaderId:     rf.me,
			PrevLogIndex: prevIndex,
			PrevLogTerm:  prevTerm,
			Entries:      entries,
			LeaderCommit: rf.commitIndex,
		}
		rf.mu.Unlock()

		var reply AppendEntryReply
		ok := rf.peers[peer].Call("Raft.AppendEntries", &args, &reply)
		if !ok {
			time.Sleep(20 * time.Millisecond)
			continue
		}

		rf.mu.Lock()
		if reply.Term > rf.currentTerm {
			// step down
			rf.currentTerm = reply.Term
			rf.state = "Follower"
			rf.votedFor = -1
			rf.persist()
			rf.mu.Unlock()
			return
		}

		if reply.Success {
			rf.nextIndex[peer] = args.PrevLogIndex + len(args.Entries) + 1
			rf.matchedIndex[peer] = rf.nextIndex[peer] - 1
			DPrintf("[%d] replicateTo %d: success, nextIndex=%d, matchedIndex=%d", rf.me, peer, rf.nextIndex[peer], rf.matchedIndex[peer])

			rf.mu.Unlock()
			rf.advanceCommitIndex()

			// Short sleep to act as heartbeat interval
			// Reduced from 50ms to 30ms to improve performance for 4B speed test
			time.Sleep(30 * time.Millisecond)
			continue
		}

		// 失败：使用优化的快速回退
		DPrintf("[%d] replicateTo %d: AppendEntries failed, prevIndex=%d, prevTerm=%d, entries=%d, reply.ConflictTerm=%d, reply.ConflictIndex=%d",
			rf.me, peer, args.PrevLogIndex, args.PrevLogTerm, len(args.Entries), reply.ConflictTerm, reply.ConflictIndex)

		if reply.ConflictTerm != 0 {
			// Leader在本地日志中查找ConflictTerm的最后一个索引
			// 优化：从后向前搜索，但考虑快照边界
			lastIndexOfTerm := -1
			for i := len(rf.log) - 1; i >= 0; i-- {
				if rf.log[i].Index <= rf.lastIncludedIndex {
					break // 不需要搜索快照之前的日志
				}
				if rf.log[i].Term == reply.ConflictTerm {
					lastIndexOfTerm = rf.log[i].Index
					break
				}
				if rf.log[i].Term < reply.ConflictTerm {
					break // Term更小，不可能找到了
				}
			}
			if lastIndexOfTerm != -1 {
				// Leader有这个Term，从该Term的下一个索引开始
				rf.nextIndex[peer] = lastIndexOfTerm + 1
			} else {
				// Leader没有这个Term，从Follower的ConflictIndex开始
				rf.nextIndex[peer] = reply.ConflictIndex
			}
		} else {
			// Follower的日志太短，直接使用ConflictIndex
			rf.nextIndex[peer] = reply.ConflictIndex
		}

		DPrintf("[%d] replicateTo %d: adjusted nextIndex=%d (lastIncludedIndex=%d)",
			rf.me, peer, rf.nextIndex[peer], rf.lastIncludedIndex)
		rf.mu.Unlock()
		// Rapidly retry with minimal delay
		time.Sleep(5 * time.Millisecond)
	}
}

func (rf *Raft) applyLoop() {
	for !rf.killed() {
		time.Sleep(5 * time.Millisecond)

		rf.mu.Lock()
		// 收集需要应用的条目（不持有锁发送）
		var messages []ApplyMsg
		for rf.lastApplied < rf.commitIndex {
			rf.lastApplied++

			if rf.lastApplied <= rf.lastIncludedIndex {
				continue
			}

			logIdx := rf.logIndex(rf.lastApplied)

			if logIdx < 0 || logIdx >= len(rf.log) {
				rf.lastApplied--
				break
			}
			entry := rf.log[logIdx]

			// 确保条目的 Index 与 lastApplied 一致
			if entry.Index != rf.lastApplied {
				// 日志索引不一致，这是一个严重错误
				rf.lastApplied--
				break
			}

			msg := ApplyMsg{
				CommandValid: true,
				Command:      entry.Command,
				CommandIndex: entry.Index,
			}
			messages = append(messages, msg)
		}
		rf.mu.Unlock()

		// 释放锁后再发送到 channel
		for _, msg := range messages {
			rf.applyCh <- msg
		}
	}
}

func (rf *Raft) advanceCommitIndex() {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	n := len(rf.peers)
	lastLogIdx, _ := rf.getLastLogInfo()
	oldCommitIndex := rf.commitIndex

	// 优化：从commitIndex+1开始向后查找，而不是从lastLogIdx向前
	// 这样通常只需要检查很少的索引
	for N := rf.commitIndex + 1; N <= lastLogIdx; N++ {
		if N <= rf.lastIncludedIndex {
			continue
		}

		logIdx := rf.logIndex(N)
		if logIdx < 0 || logIdx >= len(rf.log) {
			continue
		}

		// 只能提交当前任期的日志
		if rf.log[logIdx].Term != rf.currentTerm {
			continue
		}

		// 快速计数：有多少个peer已经复制了这个索引
		count := 1 // leader自己
		for i := 0; i < n; i++ {
			if i == rf.me {
				continue
			}
			if rf.matchedIndex[i] >= N {
				count++
			}
		}

		// 如果达到多数派，更新commitIndex并继续检查下一个
		if count > n/2 {
			rf.commitIndex = N
		} else {
			// 没达到多数派，后面的索引也不可能达到，直接退出
			break
		}
	}

	if rf.commitIndex != oldCommitIndex {
		DPrintf("[%d] advanceCommitIndex: %d -> %d, term=%d, matchedIndex=%v",
			rf.me, oldCommitIndex, rf.commitIndex, rf.currentTerm, rf.matchedIndex)
	}
}

func (rf *Raft) AppendEntries(arg *AppendEntryArgs, reply *AppendEntryReply) {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	reply.Success = false
	reply.Term = rf.currentTerm

	if arg.Term < rf.currentTerm {
		return
	}

	if arg.Term > rf.currentTerm {
		rf.currentTerm = arg.Term
		rf.state = "Follower"
		rf.votedFor = -1
		rf.persist()
	}

	rf.lastHeartbeat = time.Now()

	// 检查 PrevLog 是否匹配
	// Case 1: PrevLogIndex 在快照之前，说明leader的信息过旧
	if arg.PrevLogIndex < rf.lastIncludedIndex {
		// 返回快照后的第一个索引，让leader快速更新
		reply.ConflictIndex = rf.lastIncludedIndex + 1
		reply.ConflictTerm = 0
		return
	}

	// Case 2: PrevLogIndex == lastIncludedIndex (可能都是 0)
	if arg.PrevLogIndex == rf.lastIncludedIndex {
		DPrintf("[%d] AppendEntries: Case 2, PrevLogIndex=%d == lastIncludedIndex=%d, entries=%d",
			rf.me, arg.PrevLogIndex, rf.lastIncludedIndex, len(arg.Entries))
		// 检查 term 是否匹配
		if arg.PrevLogTerm != rf.lastIncludeTerm {
			DPrintf("[%d] AppendEntries: term mismatch, PrevLogTerm=%d != lastIncludeTerm=%d",
				rf.me, arg.PrevLogTerm, rf.lastIncludeTerm)
			reply.ConflictIndex = rf.lastIncludedIndex + 1
			return
		}
		// Term 匹配，处理新条目
		if len(arg.Entries) > 0 {
			// 确保有哨兵条目（如果需要）
			if rf.lastIncludedIndex > 0 && len(rf.log) == 0 {
				rf.log = []LogEntry{{Term: rf.lastIncludeTerm, Index: rf.lastIncludedIndex}}
			}

			// 从哨兵后开始合并（或从头开始如果没有快照）
			// insertIdx指向log数组中的位置（哨兵后或开头）
			insertIdx := 0
			if rf.lastIncludedIndex > 0 {
				insertIdx = 1 // 跳过哨兵
			}

			// 逐个检查新条目，找到冲突点
			newIdx := 0
			for newIdx < len(arg.Entries) {
				if insertIdx >= len(rf.log) {
					// 超出现有日志，直接添加剩余部分
					break
				}
				if rf.log[insertIdx].Term != arg.Entries[newIdx].Term {
					// 发现冲突，截断并添加新条目
					rf.log = rf.log[:insertIdx]
					break
				}
				insertIdx++
				newIdx++
			}

			// 添加剩余的新条目
			if newIdx < len(arg.Entries) {
				rf.log = append(rf.log, arg.Entries[newIdx:]...)
			}
			rf.persist()
			DPrintf("[%d] AppendEntries Case 2: after append, log len=%d", rf.me, len(rf.log))
		}
		reply.Success = true
		if arg.LeaderCommit > rf.commitIndex {
			lastIdx, _ := rf.getLastLogInfo()
			rf.commitIndex = min(arg.LeaderCommit, lastIdx)
			DPrintf("[%d] AppendEntries Case 2: updated commitIndex=%d", rf.me, rf.commitIndex)
		}
		return
	}

	// Case 3: PrevLogIndex > lastIncludedIndex，检查日志中的条目
	logIdx := rf.logIndex(arg.PrevLogIndex)
	if logIdx < 0 || logIdx >= len(rf.log) {
		// Follower的日志太短，返回当前日志末尾的下一个索引
		reply.ConflictTerm = 0
		lastIdx, _ := rf.getLastLogInfo()
		reply.ConflictIndex = lastIdx + 1
		DPrintf("[%d] AppendEntries: log too short, PrevLogIndex=%d, lastIdx=%d, ConflictIndex=%d",
			rf.me, arg.PrevLogIndex, lastIdx, reply.ConflictIndex)
		return
	}

	if rf.log[logIdx].Term != arg.PrevLogTerm {
		reply.ConflictTerm = rf.log[logIdx].Term
		// 找到该任期第一个索引
		for i := logIdx; i >= 0; i-- {
			if rf.log[i].Term != reply.ConflictTerm {
				reply.ConflictIndex = rf.log[i+1].Index
				return
			}
		}
		reply.ConflictIndex = rf.lastIncludedIndex + 1
		return
	}

	// 处理新条目
	if len(arg.Entries) > 0 {
		insertIdx := logIdx + 1
		newIdx := 0

		for newIdx < len(arg.Entries) {
			if insertIdx >= len(rf.log) {
				break
			}
			if rf.log[insertIdx].Term != arg.Entries[newIdx].Term {
				rf.log = rf.log[:insertIdx]
				break
			}
			insertIdx++
			newIdx++
		}

		if newIdx < len(arg.Entries) {
			rf.log = append(rf.log, arg.Entries[newIdx:]...)
		}
		rf.persist()
	}

	if arg.LeaderCommit > rf.commitIndex {
		lastIdx := arg.PrevLogIndex + len(arg.Entries)
		rf.commitIndex = min(arg.LeaderCommit, lastIdx)
	}

	reply.Success = true
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (rf *Raft) ticker() {
	for rf.killed() == false {

		// Your code here (3A)
		// Check if a leader election should be started.

		// pause for a random amount of time between 50 and 350
		// milliseconds.

		rf.mu.Lock()
		isleader := (rf.state == "Leader")
		lastheartbeat := rf.lastHeartbeat
		rf.mu.Unlock()

		if isleader {
			// Leader的心跳和日志复制由replicateTo goroutine处理
			// 这里只需短暂sleep避免占用CPU
			time.Sleep(100 * time.Millisecond)
			continue
		} else {
			// if it is not leader，then it will stay and recieve heartbeat
			// if it didnt recieve the heartbeat of leader, then it will
			// became leader and send request votes for all

			// wait for heartbeat part start

			if lastheartbeat.IsZero() {
				lastheartbeat = time.Now()
				rf.mu.Lock()
				if rf.lastHeartbeat.IsZero() {
					rf.lastHeartbeat = lastheartbeat
				}
				rf.mu.Unlock()
			}

			for {
				if rf.killed() {
					return
				}

				rf.mu.Lock()
				if rf.state == "Leader" {
					rf.mu.Unlock()
					break
				}
				lastestheartbeat := rf.lastHeartbeat
				currentTimeout := rf.ElectionTimeout
				rf.mu.Unlock()
				// if it recieved heartbeat , its lastestheartbeat will change and
				// then we reset the lastheartbeat
				if !lastheartbeat.IsZero() && lastestheartbeat.After(lastheartbeat) {
					lastheartbeat = lastestheartbeat
				}

				if time.Since(lastheartbeat) >= currentTimeout {
					go rf.startElection()
					break
				}

				time.Sleep(5 * time.Millisecond)
			}
		}

		ms := 30 + (rand.Int63() % 50)
		time.Sleep(time.Duration(ms) * time.Millisecond)
	}
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
func Make(peers []Peer, me int,
	persister Persister, applyCh chan ApplyMsg) *Raft {
	rf := &Raft{}
	rf.peers = peers
	rf.persister = persister
	rf.me = me
	rf.applyCh = applyCh

	// 先设置默认值
	rf.state = "Follower"
	rf.votedFor = -1
	rf.currentTerm = 0
	rf.commitIndex = 0
	rf.lastApplied = 0
	rf.lastIncludedIndex = 0
	rf.lastIncludeTerm = 0
	rf.log = []LogEntry{} // 初始化为空切片

	rand.Seed(time.Now().UnixNano())
	rf.ElectionTimeout = time.Duration(200+rand.Intn(301)) * time.Millisecond

	// initialize from state persisted before a crash
	// readPersist会覆盖上面设置的值
	rf.readPersist(persister.ReadRaftState())

	// 如果有快照，需要相应调整commitIndex和lastApplied
	if rf.lastIncludedIndex > 0 {
		if rf.commitIndex < rf.lastIncludedIndex {
			rf.commitIndex = rf.lastIncludedIndex
		}
		if rf.lastApplied < rf.lastIncludedIndex {
			rf.lastApplied = rf.lastIncludedIndex
		}
		// 确保日志至少有哨兵条目
		if len(rf.log) == 0 {
			rf.log = []LogEntry{{Term: rf.lastIncludeTerm, Index: rf.lastIncludedIndex}}
		}
	}

	DPrintf("[%d] Make: term=%d, votedFor=%d, logLen=%d, lastIncludedIndex=%d, commitIndex=%d",
		rf.me, rf.currentTerm, rf.votedFor, len(rf.log), rf.lastIncludedIndex, rf.commitIndex)

	go rf.ticker()
	go rf.applyLoop()

	return rf
}

// RPC通信
// *************************************************************************************
// 为 Raft 节点间通信提供基于HTTP的RPC通道，适配Raft的Peer接口，
// 承载 RequestVote 与 AppendEntries（心跳/日志复制）调用。
// *************************************************************************************
// 	主要类型：
// 		RaftRPC：用于封装节点，实现peer.Call()
// 		RaftRPCServer：在 HTTP 路由上注册 /raft/RequestVote、/raft/AppendEntries 处理函数。将Raft相关请求api单独分隔开
// 		RaftInterface：抽象Raft节点需要暴露的两个方法。
//
// 	客户端流程：
// 		将类似 "Raft.RequestVote" 的方法名去掉前缀，映射为 /raft/RequestVote 路径。
// 		然后json.Marshal(args) 序列化请求体，HTTP POST 发送。
// 		检查 HTTP 状态码 200，读取响应体并 json.Unmarshal 到 reply。
//
// 	服务端流程：
// 		RegisterHandlers 在传入的 ServeMux 上注册两个 POST 路由。
// 		处理器：
//			校验 HTTP 方法必须为 POST。
// 			json.NewDecoder 反序列化请求体到 Args。
// 			调用注入的 Raft 实例方法（RequestVote / AppendEntries）。
// 			将 Reply 序列化为 JSON 写回响应。
// 		InstallSnapshot 暂未实现（注释提示，当前返回 404）。
//
//  TODO：
// 		InstallSnapshot 相关的 RPC（客户端 Call 分支、服务端路由与处理器）尚未实现。
// 		错误处理与重试/超时策略简单，未做重试或熔断。
// 		未做鉴权/签名、请求体大小限制、日志与指标埋点。

package cluster

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// RaftRPC 实现 Raft 需要的 Peer 接口，用于节点间通信
type RaftRPC struct {
	targetAddr string
	client     *http.Client
}

func NewRaftRPC(targetAddr string) *RaftRPC {
	return &RaftRPC{
		targetAddr: targetAddr,
		client: &http.Client{
			Timeout: 2 * time.Second,
		},
	}
}

// Call 实现 Peer 接口的 Call 方法
// method: "Raft.RequestVote" 或 "Raft.AppendEntries" 或 "Raft.InstallSnapshot"
// args: RequestVoteArgs 或 AppendEntriesArgs
// reply: RequestVoteReply 或 AppendEntriesReply
func (rpc *RaftRPC) Call(method string, args interface{}, reply interface{}) bool {
	// 从 "Raft.MethodName" 提取方法名
	methodName := method
	if len(method) > 5 && method[:5] == "Raft." {
		methodName = method[5:] // 去掉 "Raft." 前缀
	}

	// 构造 HTTP 请求路径
	url := fmt.Sprintf("http://%s/raft/%s", rpc.targetAddr, methodName)

	// 序列化请求参数
	data, err := json.Marshal(args)
	if err != nil {
		fmt.Printf("RPC Call %s to %s: marshal error: %v\n", method, rpc.targetAddr, err)
		return false
	}

	// 发送 POST 请求
	resp, err := rpc.client.Post(url, "application/json", bytes.NewBuffer(data))
	if err != nil {
		fmt.Printf("RPC Call %s to %s: network error: %v\n", method, rpc.targetAddr, err)
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Printf("RPC Call %s to %s: HTTP %d\n", method, rpc.targetAddr, resp.StatusCode)
		return false
	}

	// 解析响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("RPC Call %s to %s: read error: %v\n", method, rpc.targetAddr, err)
		return false
	}

	err = json.Unmarshal(body, reply)
	if err != nil {
		fmt.Printf("RPC Call %s to %s: unmarshal error: %v\n", method, rpc.targetAddr, err)
		return false
	}

	return true
}

// RaftRPCServer 提供 Raft RPC 的 HTTP 服务端
type RaftRPCServer struct {
	raft RaftInterface // 使用接口以避免循环依赖
}

// RaftInterface 定义 Raft 需要暴露的方法
type RaftInterface interface {
	RequestVote(args *RequestVoteArgs, reply *RequestVoteReply)
	AppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply)
}

func NewRaftRPCServer(raft RaftInterface) *RaftRPCServer {
	return &RaftRPCServer{raft: raft}
}

// RegisterHandlers 注册 Raft RPC 处理器到 HTTP 服务器
func (s *RaftRPCServer) RegisterHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/raft/RequestVote", s.handleRequestVote)
	mux.HandleFunc("/raft/AppendEntries", s.handleAppendEntries)
	// InstallSnapshot 暂时不实现，返回 404
}

func (s *RaftRPCServer) handleRequestVote(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var args RequestVoteArgs
	if err := json.NewDecoder(r.Body).Decode(&args); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	var reply RequestVoteReply
	s.raft.RequestVote(&args, &reply)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(reply)
}

func (s *RaftRPCServer) handleAppendEntries(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var args AppendEntriesArgs
	if err := json.NewDecoder(r.Body).Decode(&args); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	var reply AppendEntriesReply
	s.raft.AppendEntries(&args, &reply)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(reply)
}

// 定义 Raft RPC 参数和返回值结构体
type RequestVoteArgs struct {
	Term         int `json:"Term"`
	CandidateId  int `json:"CandidateId"`
	LastLogIndex int `json:"LastLogIndex"`
	LastLogTerm  int `json:"LastLogTerm"`
}

type RequestVoteReply struct {
	Term        int  `json:"Term"`
	VoteGranted bool `json:"VoteGranted"`
}

type AppendEntriesArgs struct {
	Term         int        `json:"Term"`
	LeaderId     int        `json:"LeaderId"`
	PrevLogIndex int        `json:"PrevLogIndex"`
	PrevLogTerm  int        `json:"PrevLogTerm"`
	Entries      []LogEntry `json:"Entries"`
	LeaderCommit int        `json:"LeaderCommit"`
}

type AppendEntriesReply struct {
	Term          int  `json:"Term"`
	Success       bool `json:"Success"`
	ConflictIndex int  `json:"ConflictIndex"`
	ConflictTerm  int  `json:"ConflictTerm"`
}

type LogEntry struct {
	Index   int         `json:"Index"`
	Term    int         `json:"Term"`
	Command interface{} `json:"Command"`
}

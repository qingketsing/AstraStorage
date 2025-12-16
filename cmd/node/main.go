package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"multi_driver/internal/core/consensus/raft"
	"multi_driver/internal/core/integration"
)

func main() {
	// 解析命令行参数
	id := flag.String("id", "node-0", "node id")
	addr := flag.String("addr", "127.0.0.1:29001", "listen address for discovery/raft HTTP")
	peersStr := flag.String("peers", "", "comma-separated peer addresses (excluding self)")
	me := flag.Int("me", 0, "this node index in cluster (for raft)")
	healthPort := flag.Int("health-port", 8080, "HTTP health check port")
	dbDSN := flag.String("db-dsn", "host=localhost port=5432 user=postgres password=postgre dbname=driver sslmode=disable", "PostgreSQL Data Source Name")

	// Redis 和 RabbitMQ 配置（所有节点共享）
	redisAddr := flag.String("redis-addr", "", "Redis address (only Leader connects)")
	rabbitmqURL := flag.String("rabbitmq-url", "", "RabbitMQ URL (only Leader connects)")

	flag.Parse()

	// 解析 peers
	peerAddrs := []string{}
	if *peersStr != "" {
		parts := strings.Split(*peersStr, ",")
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				peerAddrs = append(peerAddrs, p)
			}
		}
	}

	log.Printf("启动节点 %s, 地址 %s (me=%d)", *id, *addr, *me)
	log.Printf("集群节点: %v", peerAddrs)
	if *redisAddr != "" {
		log.Printf("Redis 地址: %s (Leader 自动连接)", *redisAddr)
	}
	if *rabbitmqURL != "" {
		log.Printf("RabbitMQ 地址: %s (Leader 自动连接)", *rabbitmqURL)
	}

	// 创建 applyCh 和 persister
	applyCh := make(chan raft.ApplyMsg, 256)
	persister := raft.NewInMemoryPersister()

	// 启动节点
	node, err := integration.NewNode(*id, *addr, *me, peerAddrs, persister, applyCh, *dbDSN, *redisAddr, *rabbitmqURL)
	if err != nil {
		log.Fatalf("启动节点失败: %v", err)
	}

	// 启动 goroutine 处理已提交的日志
	go func() {
		for msg := range applyCh {
			if msg.CommandValid {
				log.Printf("[%s] 收到已提交命令: index=%d, cmd=%v",
					*id, msg.CommandIndex, msg.Command)
			}
		}
	}()

	// 启动健康检查 HTTP 服务器
	healthAddr := fmt.Sprintf(":%d", *healthPort)
	httpServer := &http.Server{Addr: healthAddr}

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	http.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		term := node.GetTerm()
		isLeader := node.IsLeader()
		status := map[string]interface{}{
			"id":        *id,
			"address":   *addr,
			"term":      term,
			"isLeader":  isLeader,
			"timestamp": time.Now().Unix(),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(status)
	})

	http.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		metrics := node.Discovery.GetHeartbeatMetrics()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(metrics)
	})

	go func() {
		log.Printf("健康检查服务启动在 %s", healthAddr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("HTTP 服务器错误: %v", err)
		}
	}()

	log.Printf("节点 %s 启动成功", *id)

	// 等待退出信号
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	log.Printf("收到退出信号，正在关闭节点 %s...", *id)

	// 优雅关闭
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(ctx); err != nil {
		log.Printf("HTTP 服务器关闭错误: %v", err)
	}

	node.Stop()
	close(applyCh)

	log.Printf("节点 %s 已停止", *id)
}

# Distributed Cache - System Design Document

## 1. Overview

A distributed, in-memory key-value cache system built in Go for learning distributed systems concepts. Designed to run locally for development and testing, with production-grade patterns.

**Target Use Case:** Developers can spin up a multi-node cache cluster locally to practice distributed systems concepts.

---

## 2. Requirements

### Functional Requirements
- Store and retrieve key-value pairs (bytes)
- Support TTL (time-to-live) for automatic expiration
- Distribute data across multiple nodes using consistent hashing
- Replicate data for fault tolerance
- Handle node failures gracefully
- Dynamic node discovery and membership

### Non-Functional Requirements
- **Latency:** < 10ms for local deployments
- **Availability:** Survive single node failures (with replication)
- **Consistency:** Configurable (eventual or quorum-based)
- **Scalability:** Horizontal scaling by adding nodes
- **Operability:** Easy to run locally with Docker Compose

### Out of Scope (v1)
- Persistence to disk
- Authentication/Authorization
- Multi-datacenter replication
- Transactions or CAS (Compare-And-Swap)

---

## 3. High-Level Architecture
```
┌──────────────────────────────────────────────────┐
│             Service Registry (etcd)               │
│  • Node membership & health                       │
│  • Heartbeat monitoring                           │
└───────────┬──────────────────────────────────────┘
            │ (register, heartbeat, query)
            │
    ┌───────┴────────┬─────────────┬──────────┐
    │                │             │          │
┌───▼────┐      ┌───▼────┐   ┌───▼────┐  ┌──▼─────┐
│ Node 1 │◄────►│ Node 2 │◄─►│ Node 3 │  │ Client │
│ :8001  │ gRPC │ :8002  │   │ :8003  │  │        │
│        │      │        │   │        │  │        │
│ [LRU]  │      │ [LRU]  │   │ [LRU]  │  └────────┘
└────────┘      └────────┘   └────────┘
   Replication via gRPC
```

### Components

1. **Cache Node:** gRPC server with in-memory LRU cache
2. **Service Registry:** Tracks active nodes (in-memory or etcd)
3. **Client Library:** Routes requests using consistent hashing
4. **Replication Layer:** Ensures data redundancy

---

## 4. Key Design Decisions

### 4.1 Client-Side vs Proxy-Based Routing

**Decision:** Client-side sharding (with optional proxy in v2)

**Rationale:**
- Lower latency (no extra hop)
- No single point of failure
- Better scalability
- Standard in production systems (Redis Cluster, Memcached)

**Trade-off:** Clients must maintain hash ring and node list

---

### 4.2 Data Distribution: Consistent Hashing

**Algorithm:** Consistent hashing with virtual nodes
```
Hash Ring (0 to 2^32):
Node1: [vnode-1-1, vnode-1-2, ..., vnode-1-100] at positions [1234, 5678, ...]
Node2: [vnode-2-1, vnode-2-2, ..., vnode-2-100] at positions [2345, 6789, ...]
Node3: [vnode-3-1, vnode-3-2, ..., vnode-3-100] at positions [3456, 7890, ...]

Key "user:123" → hash(key) = 4000 → walks clockwise → finds vnode-1-50 → Node1
```

**Parameters:**
- Virtual nodes per physical node: **150** (balance between distribution and overhead)
- Hash function: **FNV-1a** (fast, good distribution)

**Benefits:**
- Adding/removing nodes only affects ~1/N of keys
- Better load distribution with few physical nodes
- Deterministic placement (all clients agree)

---

### 4.3 Replication Strategy

**Decision:** Configurable async (default) or sync replication

#### Async Replication (Default)
```go
Write Flow:
1. Client → Primary node (Set key-value)
2. Primary writes to local cache
3. Primary returns SUCCESS to client immediately
4. Primary replicates to replicas in background goroutine
```

**Configuration:**
```yaml
replication:
  strategy: "async"
  factor: 3          # Total copies (1 primary + 2 replicas)
```

**Trade-offs:**
- ✅ Low latency (~1-2ms)
- ✅ High throughput
- ❌ Risk of data loss if primary crashes before replication
- ❌ Eventual consistency (replicas lag)

#### Sync Replication (Optional)
```go
Write Flow:
1. Client → Primary node
2. Primary writes locally
3. Primary replicates to replicas (parallel)
4. Wait for write_quorum acknowledgments
5. Return SUCCESS if quorum met
```

**Configuration:**
```yaml
replication:
  strategy: "sync"
  factor: 3
  write_quorum: 2    # Must succeed on 2 nodes (including primary)
  timeout_ms: 100
```

**Quorum Math:** W + R > N ensures consistency
- N=3, W=2, R=2 → Strong consistency (can tolerate 1 failure)

**Trade-offs:**
- ✅ Stronger consistency
- ✅ Less data loss risk
- ❌ Higher latency (~5-10ms)
- ❌ Lower throughput

---

### 4.4 Replica Placement

**Strategy:** Next N-1 nodes clockwise on the hash ring
```
Hash Ring:
... → Node1 → Node2 → Node3 → Node1 → ...

Key "user:123" → Primary: Node1
              → Replicas: [Node2, Node3]
```

**Rationale:**
- Simple to implement
- Deterministic (all clients agree)
- Automatic rebalancing when nodes join/leave

---

### 4.5 Cache Eviction Policy

**Algorithm:** LRU (Least Recently Used)

**Implementation:**
- Doubly-linked list for access order
- Hash map for O(1) lookups
- Move to front on Get/Set

**Memory Management:**
```yaml
cache:
  max_size_mb: 512
  eviction_policy: "LRU"
```

**Why LRU?**
- Simple and predictable
- Works well for general workloads
- O(1) eviction

*(v2 could add LFU, FIFO, or TTL-only)*

---

### 4.6 Service Discovery

**Decision:** Registry interface with pluggable implementations
```go
type Registry interface {
    Register(node NodeInfo) error
    Heartbeat(nodeID string) error
    ListNodes() ([]NodeInfo, error)
    Deregister(nodeID string) error
}
```

**Implementations:**

#### In-Memory Registry (v1, default)
- No external dependencies
- Perfect for local development
- Single process (not distributed)

#### etcd Registry (v2)
- Production-ready
- Distributed consensus
- Watch API for membership changes
- TTL-based leases

**Configuration:**
```yaml
registry:
  type: "memory"              # or "etcd"
  endpoints: ["localhost:2379"]
  heartbeat_interval_s: 10
  node_ttl_s: 30
```

---

### 4.7 Failure Detection & Recovery

#### Health Checks
- Nodes send heartbeat every **10 seconds**
- Registry marks node UNHEALTHY if no heartbeat for **30 seconds**
- Clients refresh node list every **5 seconds**

#### Recovery Mechanisms

**v1: Read Repair (Lazy)**
```
Client reads key → Primary miss → Query replica → Hit
→ Primary learns key during read
```

**v2: Anti-Entropy (Proactive)**
- Nodes periodically sync with replicas
- Use Merkle trees for efficient comparison

**v3: Hinted Handoff**
- When node is down, writes go to temporary node with "hint"
- Forwarded when node recovers

---

## 5. API Design

### gRPC Services

#### CacheService (cache.proto)
```protobuf
service CacheService {
  rpc Get(GetRequest) returns (GetResponse);
  rpc Set(SetRequest) returns (SetResponse);
  rpc Delete(DeleteRequest) returns (DeleteResponse);
  rpc HealthCheck(HealthCheckRequest) returns (HealthCheckResponse);
}
```

#### NodeRegistryService (registry.proto)
```protobuf
service NodeRegistryService {
  rpc RegisterNode(RegisterNodeRequest) returns (RegisterNodeResponse);
  rpc Heartbeat(HeartbeatRequest) returns (HeartbeatResponse);
  rpc ListNodes(ListNodesRequest) returns (ListNodesResponse);
}
```

---

## 6. Data Flows

### Write Path (Async Replication)
```
1. Client computes hash(key) → identifies primary & replicas
2. Client → gRPC Set(key, value) → Primary
3. Primary:
   a. Writes to local LRU cache
   b. Returns SUCCESS to client
   c. Spawns goroutine:
      - gRPC Replicate(key, value) → Replica1
      - gRPC Replicate(key, value) → Replica2
```

**Latency:** ~1-2ms (local network)

---

### Read Path
```
1. Client computes hash(key) → identifies primary
2. Client → gRPC Get(key) → Primary
3. Primary:
   a. Lookup in local cache
   b. If found → return value
   c. If miss → [v2: query replicas for read repair]
```

**Latency:** ~1ms (cache hit)

---

### Node Join
```
1. New node starts
2. Node → gRPC RegisterNode() → Registry
3. Registry:
   a. Adds node to membership list
   b. Returns current node list
4. New node:
   a. Builds hash ring from node list
   b. Starts serving requests
5. Existing clients:
   a. Refresh node list (next poll)
   b. Rebuild hash ring
   c. Keys rehash to new topology
```

**Note:** In v1, no data migration. Keys naturally migrate as they're written.

---

### Node Failure
```
1. Node crashes (stops sending heartbeats)
2. Registry marks node UNHEALTHY after 30s
3. Clients refresh node list
4. Clients rebuild hash ring (node removed)
5. Reads/writes go to replicas
6. [v2: Read repair fills gaps]
```

---

## 7. Configuration

### Node Configuration (config.yaml)
```yaml
node:
  id: "node-1"                    # Unique identifier
  address: "localhost:8001"       # gRPC listen address
  
cache:
  max_size_mb: 512
  eviction_policy: "LRU"
  default_ttl_s: 3600             # 1 hour
  
replication:
  strategy: "async"               # "async" or "sync"
  factor: 3
  write_quorum: 2                 # For sync only
  timeout_ms: 100
  
registry:
  type: "memory"                  # "memory" or "etcd"
  endpoints: ["localhost:2379"]
  heartbeat_interval_s: 10
  node_ttl_s: 30

metrics:
  enabled: true
  port: 9090                      # Prometheus metrics

logging:
  level: "info"                   # debug, info, warn, error
  format: "json"
```

---

## 8. Observability

### Metrics (Prometheus)
```
# Cache metrics
cache_hits_total
cache_misses_total
cache_evictions_total
cache_size_bytes
cache_keys_total

# Replication metrics
replication_lag_seconds
replication_failures_total

# Node metrics
node_status (healthy=1, unhealthy=0)
grpc_requests_total{method, status}
grpc_request_duration_seconds{method}
```

### Logging
- Structured JSON logs (zap)
- Request IDs for tracing
- Key operations logged: Set, Get, Eviction, Replication

---

## 9. Testing Strategy

### Unit Tests
- Cache operations (LRU, TTL)
- Consistent hashing (distribution, node add/remove)
- Replication strategies

### Integration Tests
- Multi-node cluster setup
- Node failure scenarios
- Data distribution verification

### Performance Tests
- Throughput benchmarks
- Latency percentiles (p50, p95, p99)
- Memory usage under load

---

## 10. Deployment

### Local Development (Docker Compose)
```yaml
version: '3'
services:
  node1:
    build: .
    ports: ["8001:8001", "9091:9090"]
    environment:
      - NODE_ID=node-1
  node2:
    build: .
    ports: ["8002:8001", "9092:9090"]
  node3:
    build: .
    ports: ["8003:8001", "9093:9090"]
```

**Start cluster:**
```bash
docker-compose up -d
```

---

## 11. Future Enhancements (v2+)

- [ ] Sync replication with quorum
- [ ] Read repair / anti-entropy
- [ ] etcd-based service discovery
- [ ] Kubernetes deployment (StatefulSet)
- [ ] Persistence (RocksDB backend)
- [ ] Client connection pooling
- [ ] Hot key detection
- [ ] Admin API (metrics, key distribution)
- [ ] Proxy mode (optional coordinator)

---

## 12. References

- [Consistent Hashing](https://en.wikipedia.org/wiki/Consistent_hashing)
- [Dynamo: Amazon's Highly Available Key-value Store](https://www.allthingsdistributed.com/files/amazon-dynamo-sosp2007.pdf)
- [Redis Cluster Specification](https://redis.io/docs/reference/cluster-spec/)
- [gRPC Best Practices](https://grpc.io/docs/guides/performance/)
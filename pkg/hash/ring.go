package hash

import (
	"errors"
	"fmt"
	"hash/fnv"
	"sort"
	"sync"
)

var (
	// ErrEmptyRing indicates no nodes are available in the ring
	ErrEmptyRing = errors.New("hash ring is empty")
	// ErrNodeNotFound indicates the specified node does not exist
	ErrNodeNotFound = errors.New("node not found in ring")
	// ErrInvalidReplicaCount indicates an invalid replica count was provided
	ErrInvalidReplicaCount = errors.New("replica count must be positive")
)

// Node represents a physical node in the distributed system
type Node struct {
	ID      string // Unique identifier for the node
	Address string // Network address (host:port)
}

// Ring implements consistent hashing with virtual nodes
type Ring struct {
	mu sync.RWMutex

	// Virtual nodes per physical node (typically 100-300 for good distribution)
	vnodeCount int

	// Hash ring: sorted list of hash values
	hashRing []uint32

	// Maps hash values to physical nodes
	vnodeMap map[uint32]*Node

	// Maps node IDs to their virtual node hashes (for efficient removal)
	nodeVNodes map[string][]uint32

	// Active physical nodes
	nodes map[string]*Node
}

// Config holds configuration for the hash ring
type Config struct {
	// VirtualNodeCount is the number of virtual nodes per physical node
	// Higher values provide better distribution but use more memory
	// Recommended: 150-300
	VirtualNodeCount int
}

// DefaultConfig returns a reasonable default configuration
func DefaultConfig() Config {
	return Config{
		VirtualNodeCount: 150,
	}
}

// NewRing creates a new consistent hash ring with the given configuration
func NewRing(config Config) (*Ring, error) {
	if config.VirtualNodeCount <= 0 {
		return nil, fmt.Errorf("%w: got %d", ErrInvalidReplicaCount, config.VirtualNodeCount)
	}

	return &Ring{
		vnodeCount: config.VirtualNodeCount,
		vnodeMap:   make(map[uint32]*Node),
		nodeVNodes: make(map[string][]uint32),
		nodes:      make(map[string]*Node),
		hashRing:   make([]uint32, 0),
	}, nil
}

// AddNode adds a physical node to the ring and creates its virtual nodes
func (r *Ring) AddNode(node *Node) error {
	if node == nil {
		return errors.New("node cannot be nil")
	}
	if node.ID == "" {
		return errors.New("node ID cannot be empty")
	}
	if node.Address == "" {
		return errors.New("node address cannot be empty")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Check if the node already exists
	if _, exists := r.nodes[node.ID]; exists {
		return fmt.Errorf("node %s already exists in ring", node.ID)
	}

	// Create virtual nodes for this physical node
	vnodeHashes := make([]uint32, 0, r.vnodeCount)
	for i := 0; i < r.vnodeCount; i++ {
		// Create unique virtual node identifier: "nodeID:vnode-index"
		vnodeKey := fmt.Sprintf("%s:vnode-%d", node.ID, i)
		hash := r.hash(vnodeKey)

		// Check for hash collisions (rare but possible)
		if existingNode, exists := r.vnodeMap[hash]; exists {
			return fmt.Errorf("hash collision: vnode %s conflicts with existing node %s",
				vnodeKey, existingNode.ID)
		}

		vnodeHashes = append(vnodeHashes, hash)
		r.vnodeMap[hash] = node
		r.hashRing = append(r.hashRing, hash)
	}

	// Sort the hash ring to enable binary search
	sort.Slice(r.hashRing, func(i, j int) bool {
		return r.hashRing[i] < r.hashRing[j]
	})

	// Store node and its virtual node mappings
	r.nodes[node.ID] = node
	r.nodeVNodes[node.ID] = vnodeHashes

	return nil
}

// RemoveNode removes a physical node and all its virtual nodes from the ring
func (r *Ring) RemoveNode(nodeID string) error {
	if nodeID == "" {
		return errors.New("node ID cannot be empty")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Check if node exists
	vnodeHashes, exists := r.nodeVNodes[nodeID]
	if !exists {
		return fmt.Errorf("%w: %s", ErrNodeNotFound, nodeID)
	}

	// Remove all virtual nodes from the ring
	for _, hash := range vnodeHashes {
		delete(r.vnodeMap, hash)
	}

	// Remove hashes from the sorted ring
	r.hashRing = r.removeHashes(r.hashRing, vnodeHashes)

	// Remove node tracking
	delete(r.nodes, nodeID)
	delete(r.nodeVNodes, nodeID)

	return nil
}

// GetNode returns the primary node responsible for the given key
func (r *Ring) GetNode(key string) (*Node, error) {
	if key == "" {
		return nil, errors.New("key cannot be empty")
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(r.hashRing) == 0 {
		return nil, ErrEmptyRing
	}

	hash := r.hash(key)
	idx := r.search(hash)

	return r.vnodeMap[r.hashRing[idx]], nil
}

// GetNodes returns N nodes responsible for the given key (primary + replicas)
// The nodes are returned in clockwise order on the ring
func (r *Ring) GetNodes(key string, count int) ([]*Node, error) {
	if key == "" {
		return nil, errors.New("key cannot be empty")
	}
	if count <= 0 {
		return nil, fmt.Errorf("count must be positive, got %d", count)
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(r.hashRing) == 0 {
		return nil, ErrEmptyRing
	}

	// Limit count to available physical nodes
	physicalNodeCount := len(r.nodes)
	if count > physicalNodeCount {
		count = physicalNodeCount
	}

	hash := r.hash(key)
	idx := r.search(hash)

	// Collect unique physical nodes in clockwise order
	uniqueNodes := make([]*Node, 0, count)
	seenNodes := make(map[string]bool)
	ringLen := len(r.hashRing)

	for i := 0; len(uniqueNodes) < count && i < ringLen; i++ {
		vnodeIdx := (idx + i) % ringLen
		node := r.vnodeMap[r.hashRing[vnodeIdx]]

		// Only add if we haven't seen this physical node yet
		if !seenNodes[node.ID] {
			uniqueNodes = append(uniqueNodes, node)
			seenNodes[node.ID] = true
		}
	}

	return uniqueNodes, nil
}

// Nodes returns all physical nodes in the ring
func (r *Ring) Nodes() []*Node {
	r.mu.RLock()
	defer r.mu.RUnlock()

	nodes := make([]*Node, 0, len(r.nodes))
	for _, node := range r.nodes {
		nodes = append(nodes, node)
	}
	return nodes
}

// Size returns the number of physical nodes in the ring
func (r *Ring) Size() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.nodes)
}

// hash computes a 32-bit FNV-1a hash of the input string
// FNV-1a is fast and provides good distribution for hash tables
func (r *Ring) hash(key string) uint32 {
	h := fnv.New32a()
	_, err := h.Write([]byte(key))
	if err != nil {
		return 0
	}
	return h.Sum32()
}

// search performs binary search to find the first hash >= target
// Returns the index in the hash ring (wraps around to 0 if not found)
func (r *Ring) search(target uint32) int {
	idx := sort.Search(len(r.hashRing), func(i int) bool {
		return r.hashRing[i] >= target
	})

	// Wrap around if we've gone past the end
	if idx >= len(r.hashRing) {
		idx = 0
	}

	return idx
}

// removeHashes removes specified hashes from a sorted slice
func (r *Ring) removeHashes(ring []uint32, toRemove []uint32) []uint32 {
	if len(toRemove) == 0 {
		return ring
	}

	// Create a set for O(1) lookup
	removeSet := make(map[uint32]bool, len(toRemove))
	for _, hash := range toRemove {
		removeSet[hash] = true
	}

	// Filter out the hashes to remove
	result := make([]uint32, 0, len(ring)-len(toRemove))
	for _, hash := range ring {
		if !removeSet[hash] {
			result = append(result, hash)
		}
	}

	return result
}

// Stats returns statistics about the hash ring
type Stats struct {
	PhysicalNodes int     // Number of physical nodes
	VirtualNodes  int     // Total virtual nodes
	VNodesPerNode int     // Virtual nodes per physical node
	LoadImbalance float64 // Standard deviation of key distribution (0 = perfect balance)
}

// GetStats returns current statistics about the ring
func (r *Ring) GetStats() Stats {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return Stats{
		PhysicalNodes: len(r.nodes),
		VirtualNodes:  len(r.hashRing),
		VNodesPerNode: r.vnodeCount,
	}
}

package hash

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRing_ValidConfig(t *testing.T) {
	config := Config{VirtualNodeCount: 150}
	ring, err := NewRing(config)

	require.NoError(t, err, "NewRing should not return error with valid config")
	assert.NotNil(t, ring, "Ring should not be nil")
	assert.Equal(t, 0, ring.Size(), "New ring should be empty")
}

func TestNewRing_InvalidConfig(t *testing.T) {
	tests := []struct {
		name   string
		config Config
	}{
		{"zero vnodes", Config{VirtualNodeCount: 0}},
		{"negative vnodes", Config{VirtualNodeCount: -1}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ring, err := NewRing(tt.config)
			assert.Error(t, err, "NewRing should return error for invalid config")
			assert.Nil(t, ring, "Ring should be nil on error")
		})
	}
}

func TestRing_AddNode_Success(t *testing.T) {
	ring, err := NewRing(DefaultConfig())
	require.NoError(t, err)

	node := &Node{ID: "node1", Address: "localhost:8001"}
	err = ring.AddNode(node)

	require.NoError(t, err, "AddNode should not return error")
	assert.Equal(t, 1, ring.Size(), "Ring size should be 1")

	stats := ring.GetStats()
	assert.Equal(t, 1, stats.PhysicalNodes)
	assert.Equal(t, 150, stats.VirtualNodes)
}

func TestRing_AddNode_InvalidInput(t *testing.T) {
	ring, err := NewRing(DefaultConfig())
	require.NoError(t, err)

	tests := []struct {
		name string
		node *Node
	}{
		{"nil node", nil},
		{"empty ID", &Node{ID: "", Address: "localhost:8001"}},
		{"empty address", &Node{ID: "node1", Address: ""}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ring.AddNode(tt.node)
			assert.Error(t, err, "AddNode should return error for invalid input")
		})
	}
}

func TestRing_AddNode_Duplicate(t *testing.T) {
	ring, err := NewRing(DefaultConfig())
	require.NoError(t, err)

	node := &Node{ID: "node1", Address: "localhost:8001"}
	
	err = ring.AddNode(node)
	require.NoError(t, err, "First AddNode should succeed")

	err = ring.AddNode(node)
	assert.Error(t, err, "Adding duplicate node should return error")
	assert.Equal(t, 1, ring.Size(), "Ring size should remain 1")
}

func TestRing_RemoveNode_Success(t *testing.T) {
	ring, err := NewRing(DefaultConfig())
	require.NoError(t, err)

	node := &Node{ID: "node1", Address: "localhost:8001"}
	err = ring.AddNode(node)
	require.NoError(t, err)

	err = ring.RemoveNode("node1")
	require.NoError(t, err, "RemoveNode should not return error")
	assert.Equal(t, 0, ring.Size(), "Ring should be empty after removal")

	stats := ring.GetStats()
	assert.Equal(t, 0, stats.PhysicalNodes)
	assert.Equal(t, 0, stats.VirtualNodes)
}

func TestRing_RemoveNode_NotFound(t *testing.T) {
	ring, err := NewRing(DefaultConfig())
	require.NoError(t, err)

	err = ring.RemoveNode("nonexistent")
	assert.ErrorIs(t, err, ErrNodeNotFound, "RemoveNode should return ErrNodeNotFound")
}

func TestRing_RemoveNode_EmptyID(t *testing.T) {
	ring, err := NewRing(DefaultConfig())
	require.NoError(t, err)

	err = ring.RemoveNode("")
	assert.Error(t, err, "RemoveNode with empty ID should return error")
}

func TestRing_GetNode_SingleNode(t *testing.T) {
	ring, err := NewRing(DefaultConfig())
	require.NoError(t, err)

	node := &Node{ID: "node1", Address: "localhost:8001"}
	err = ring.AddNode(node)
	require.NoError(t, err)

	// All keys should map to the only node
	keys := []string{"key1", "key2", "key3", "user:123", "session:abc"}
	for _, key := range keys {
		result, err := ring.GetNode(key)
		require.NoError(t, err, "GetNode should not return error for key: %s", key)
		assert.Equal(t, "node1", result.ID, "All keys should map to node1")
	}
}

func TestRing_GetNode_MultipleNodes(t *testing.T) {
	ring, err := NewRing(DefaultConfig())
	require.NoError(t, err)

	// Add three nodes
	nodes := []*Node{
		{ID: "node1", Address: "localhost:8001"},
		{ID: "node2", Address: "localhost:8002"},
		{ID: "node3", Address: "localhost:8003"},
	}

	for _, node := range nodes {
		err := ring.AddNode(node)
		require.NoError(t, err)
	}

	// Test key distribution
	keyToNode := make(map[string]string)
	keys := []string{"key1", "key2", "key3", "user:123", "session:abc"}

	for _, key := range keys {
		result, err := ring.GetNode(key)
		require.NoError(t, err, "GetNode should not return error for key: %s", key)
		keyToNode[key] = result.ID
	}

	// Same key should always map to same node
	for _, key := range keys {
		result, err := ring.GetNode(key)
		require.NoError(t, err)
		assert.Equal(t, keyToNode[key], result.ID, "Key %s should consistently map to same node", key)
	}

	// Not all keys should map to the same node (statistical check)
	uniqueNodes := make(map[string]bool)
	for _, nodeID := range keyToNode {
		uniqueNodes[nodeID] = true
	}
	assert.Greater(t, len(uniqueNodes), 1, "Keys should be distributed across multiple nodes")
}

func TestRing_GetNode_EmptyRing(t *testing.T) {
	ring, err := NewRing(DefaultConfig())
	require.NoError(t, err)

	_, err = ring.GetNode("key1")
	assert.ErrorIs(t, err, ErrEmptyRing, "GetNode should return ErrEmptyRing")
}

func TestRing_GetNode_EmptyKey(t *testing.T) {
	ring, err := NewRing(DefaultConfig())
	require.NoError(t, err)

	node := &Node{ID: "node1", Address: "localhost:8001"}
	err = ring.AddNode(node)
	require.NoError(t, err)

	_, err = ring.GetNode("")
	assert.Error(t, err, "GetNode should return error for empty key")
}

func TestRing_GetNodes_ReplicationFactor(t *testing.T) {
	ring, err := NewRing(DefaultConfig())
	require.NoError(t, err)

	// Add three nodes
	for i := 1; i <= 3; i++ {
		node := &Node{
			ID:      fmt.Sprintf("node%d", i),
			Address: fmt.Sprintf("localhost:800%d", i),
		}
		err := ring.AddNode(node)
		require.NoError(t, err)
	}

	// Request 2 replicas
	nodes, err := ring.GetNodes("key1", 2)
	require.NoError(t, err, "GetNodes should not return error")
	assert.Len(t, nodes, 2, "Should return 2 nodes")

	// Verify nodes are unique
	nodeIDs := make(map[string]bool)
	for _, node := range nodes {
		assert.False(t, nodeIDs[node.ID], "Nodes should be unique")
		nodeIDs[node.ID] = true
	}
}

func TestRing_GetNodes_ExceedsAvailableNodes(t *testing.T) {
	ring, err := NewRing(DefaultConfig())
	require.NoError(t, err)

	// Add 2 nodes
	for i := 1; i <= 2; i++ {
		node := &Node{
			ID:      fmt.Sprintf("node%d", i),
			Address: fmt.Sprintf("localhost:800%d", i),
		}
		err := ring.AddNode(node)
		require.NoError(t, err)
	}

	// Request 5 replicas (more than available)
	nodes, err := ring.GetNodes("key1", 5)
	require.NoError(t, err, "GetNodes should not return error")
	assert.Len(t, nodes, 2, "Should return only available nodes (2)")
}

func TestRing_GetNodes_InvalidInput(t *testing.T) {
	ring, err := NewRing(DefaultConfig())
	require.NoError(t, err)

	node := &Node{ID: "node1", Address: "localhost:8001"}
	err = ring.AddNode(node)
	require.NoError(t, err)

	tests := []struct {
		name  string
		key   string
		count int
	}{
		{"empty key", "", 1},
		{"zero count", "key1", 0},
		{"negative count", "key1", -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ring.GetNodes(tt.key, tt.count)
			assert.Error(t, err, "GetNodes should return error for invalid input")
		})
	}
}

func TestRing_GetNodes_EmptyRing(t *testing.T) {
	ring, err := NewRing(DefaultConfig())
	require.NoError(t, err)

	_, err = ring.GetNodes("key1", 2)
	assert.ErrorIs(t, err, ErrEmptyRing, "GetNodes should return ErrEmptyRing")
}

func TestRing_Nodes(t *testing.T) {
	ring, err := NewRing(DefaultConfig())
	require.NoError(t, err)

	// Empty ring
	nodes := ring.Nodes()
	assert.Len(t, nodes, 0, "Empty ring should return empty slice")

	// Add nodes
	for i := 1; i <= 3; i++ {
		node := &Node{
			ID:      fmt.Sprintf("node%d", i),
			Address: fmt.Sprintf("localhost:800%d", i),
		}
		err := ring.AddNode(node)
		require.NoError(t, err)
	}

	nodes = ring.Nodes()
	assert.Len(t, nodes, 3, "Should return all 3 nodes")

	// Verify all nodes are present
	nodeIDs := make(map[string]bool)
	for _, node := range nodes {
		nodeIDs[node.ID] = true
	}
	assert.True(t, nodeIDs["node1"], "node1 should be present")
	assert.True(t, nodeIDs["node2"], "node2 should be present")
	assert.True(t, nodeIDs["node3"], "node3 should be present")
}

func TestRing_ConsistentMapping(t *testing.T) {
	ring, err := NewRing(DefaultConfig())
	require.NoError(t, err)

	// Add nodes
	for i := 1; i <= 3; i++ {
		node := &Node{
			ID:      fmt.Sprintf("node%d", i),
			Address: fmt.Sprintf("localhost:800%d", i),
		}
		err := ring.AddNode(node)
		require.NoError(t, err)
	}

	// Map keys to nodes
	keys := make([]string, 100)
	firstMapping := make(map[string]string)
	
	for i := 0; i < 100; i++ {
		keys[i] = fmt.Sprintf("key%d", i)
		node, err := ring.GetNode(keys[i])
		require.NoError(t, err)
		firstMapping[keys[i]] = node.ID
	}

	// Verify mapping is consistent across multiple calls
	for i := 0; i < 10; i++ {
		for _, key := range keys {
			node, err := ring.GetNode(key)
			require.NoError(t, err)
			assert.Equal(t, firstMapping[key], node.ID,
				"Key %s should consistently map to %s", key, firstMapping[key])
		}
	}
}

func TestRing_MinimalRehashing(t *testing.T) {
	ring, err := NewRing(DefaultConfig())
	require.NoError(t, err)

	// Add initial nodes
	for i := 1; i <= 3; i++ {
		node := &Node{
			ID:      fmt.Sprintf("node%d", i),
			Address: fmt.Sprintf("localhost:800%d", i),
		}
		err := ring.AddNode(node)
		require.NoError(t, err)
	}

	// Map 1000 keys
	keys := make([]string, 1000)
	initialMapping := make(map[string]string)
	
	for i := 0; i < 1000; i++ {
		keys[i] = fmt.Sprintf("key%d", i)
		node, err := ring.GetNode(keys[i])
		require.NoError(t, err)
		initialMapping[keys[i]] = node.ID
	}

	// Add a new node
	newNode := &Node{ID: "node4", Address: "localhost:8004"}
	err = ring.AddNode(newNode)
	require.NoError(t, err)

	// Check how many keys changed
	changedKeys := 0
	for _, key := range keys {
		node, err := ring.GetNode(key)
		require.NoError(t, err)
		if node.ID != initialMapping[key] {
			changedKeys++
		}
	}

	// With consistent hashing, only ~1/N keys should change (N = number of nodes)
	// Allow some variance: expect between 15-35% (ideal is 25% for 4 nodes)
	changePercentage := float64(changedKeys) / float64(len(keys)) * 100
	t.Logf("Changed keys: %d/%d (%.2f%%)", changedKeys, len(keys), changePercentage)
	
	assert.Greater(t, changePercentage, 10.0, "Too few keys changed (poor distribution)")
	assert.Less(t, changePercentage, 40.0, "Too many keys changed (not minimal rehashing)")
}

func TestRing_ConcurrentAccess(t *testing.T) {
	ring, err := NewRing(DefaultConfig())
	require.NoError(t, err)

	// Add initial nodes
	for i := 1; i <= 3; i++ {
		node := &Node{
			ID:      fmt.Sprintf("node%d", i),
			Address: fmt.Sprintf("localhost:800%d", i),
		}
		err := ring.AddNode(node)
		require.NoError(t, err)
	}

	done := make(chan bool, 3)

	// Concurrent readers
	go func() {
		for i := 0; i < 100; i++ {
			key := fmt.Sprintf("key%d", i%50)
			_, err := ring.GetNode(key)
			assert.NoError(t, err)
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 100; i++ {
			key := fmt.Sprintf("key%d", i%50)
			_, err := ring.GetNodes(key, 2)
			assert.NoError(t, err)
		}
		done <- true
	}()

	// Concurrent node list reader
	go func() {
		for i := 0; i < 100; i++ {
			nodes := ring.Nodes()
			assert.GreaterOrEqual(t, len(nodes), 3)
		}
		done <- true
	}()

	// Wait for all goroutines
	for i := 0; i < 3; i++ {
		<-done
	}
}

func BenchmarkRing_GetNode(b *testing.B) {
	ring, _ := NewRing(DefaultConfig())
	
	for i := 1; i <= 10; i++ {
		node := &Node{
			ID:      fmt.Sprintf("node%d", i),
			Address: fmt.Sprintf("localhost:800%d", i),
		}
		ring.AddNode(node)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("key%d", i)
		ring.GetNode(key)
	}
}

func BenchmarkRing_GetNodes(b *testing.B) {
	ring, _ := NewRing(DefaultConfig())
	
	for i := 1; i <= 10; i++ {
		node := &Node{
			ID:      fmt.Sprintf("node%d", i),
			Address: fmt.Sprintf("localhost:800%d", i),
		}
		ring.AddNode(node)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("key%d", i)
		ring.GetNodes(key, 3)
	}
}

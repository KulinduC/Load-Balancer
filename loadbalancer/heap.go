package loadbalancer

type ServerNode struct {
	Index       int
	Connections int
	Weight      int // negative for max-heap
}

// Comparer interface defines how to compare ServerNodes“
type Comparer interface {
	Less(ServerNode, ServerNode) bool
}

// LeastConnectionsComparer compares by connection count (min-heap)
type LeastConnectionsComparer struct{}

func (c LeastConnectionsComparer) Less(a, b ServerNode) bool {
	return a.Connections < b.Connections
}

// WeightComparer compares by weight (min-heap on negative weights = max-heap)
type WeightComparer struct{}

func (c WeightComparer) Less(a, b ServerNode) bool {
	return a.Weight < b.Weight
}

// ServerHeap is a generic heap that uses a Comparer
type ServerHeap struct {
	nodes    []*ServerNode
	comparer Comparer
}

func (h ServerHeap) Len() int { return len(h.nodes) }

func (h ServerHeap) Less(i, j int) bool {
	return h.comparer.Less(*h.nodes[i], *h.nodes[j])
}

func (h ServerHeap) Swap(i, j int) {
	h.nodes[i], h.nodes[j] = h.nodes[j], h.nodes[i]
}

func (h *ServerHeap) Push(x interface{}) {
	h.nodes = append(h.nodes, x.(*ServerNode))
}

func (h *ServerHeap) Pop() interface{} {
	old := h.nodes
	n := len(old)
	x := old[n-1]
	h.nodes = old[0 : n-1]
	return x
}

func (h *ServerHeap) Top() *ServerNode {
	if len(h.nodes) == 0 {
		return nil
	}
	return h.nodes[0]
}

// Constructor for creating a new heap with a specific comparer
func ServerHeapConstructor(comparer Comparer) *ServerHeap {
	return &ServerHeap{
		nodes:    make([]*ServerNode, 0),
		comparer: comparer,
	}
}

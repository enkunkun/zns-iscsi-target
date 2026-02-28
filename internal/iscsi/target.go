package iscsi

// Target holds metadata about an iSCSI target.
type Target struct {
	IQN    string
	Portal string
	LUNs   []LUNInfo
}

// LUNInfo describes a single LUN exposed by the target.
type LUNInfo struct {
	ID         uint32
	BlockSize  uint32
	NumBlocks  uint64
}

// NewTarget creates a new Target with the given IQN and portal address.
func NewTarget(iqn, portal string) *Target {
	return &Target{
		IQN:    iqn,
		Portal: portal,
	}
}

// AddLUN adds a LUN to the target.
func (t *Target) AddLUN(id uint32, blockSize uint32, numBlocks uint64) {
	t.LUNs = append(t.LUNs, LUNInfo{
		ID:        id,
		BlockSize: blockSize,
		NumBlocks: numBlocks,
	})
}

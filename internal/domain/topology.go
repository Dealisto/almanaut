package domain

// TopologyScope selects the devices whose recorded physical cables are shown.
// Their directly connected peers are included even when outside this scope.
// Exactly one filter may be set; no filter selects the inventory overview.
type TopologyScope struct {
	SiteID     int64
	RackID     int64
	Unassigned bool
	Device     EntityRef
}

// TopologyNode is a bounded projection of a host or hardware record.
type TopologyNode struct {
	Ref      EntityRef
	Name     string
	RackName string
	SiteName string
	Outside  bool
	Missing  bool
}

// TopologyCable represents one recorded port pair, not a routed path.
type TopologyCable struct {
	From, To         EntityRef
	FromPort, ToPort int64
	FromName, ToName string
}

// Topology is a bounded, consistent inventory view. Truncated means a narrower
// scope is needed. Broken counts invalid port links encountered in that view.
type Topology struct {
	Nodes            []TopologyNode
	Cables           []TopologyCable
	Truncated        bool
	Broken           int
	Sites, Racks     []TopologyChoice
	ChoicesTruncated bool
}

// TopologyChoice names a scope without requiring users to enter record IDs.
type TopologyChoice struct {
	ID   int64
	Name string
}

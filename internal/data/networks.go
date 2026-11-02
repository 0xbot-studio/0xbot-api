package data

// Network metadata is shared by validation, adapters and the public catalog.
type Network struct{ ID, Name, ChainHex, Symbol, Asset string }

func Networks() []Network {
	return []Network{
		{"eip155:1", "Ethereum", "0x1", "ETH", "eip155:1/slip44:60"},
		{"eip155:56", "BNB Smart Chain", "0x38", "BNB", "eip155:56/slip44:714"},
		{"eip155:137", "Polygon PoS", "0x89", "POL", "eip155:137/slip44:966"},
	}
}

func FindNetwork(id string) (Network, bool) {
	for _, n := range Networks() {
		if n.ID == id {
			return n, true
		}
	}
	return Network{}, false
}

func NetworkIDs() []string {
	ids := []string{}
	for _, n := range Networks() {
		ids = append(ids, n.ID)
	}
	return ids
}

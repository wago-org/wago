//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

type managedFunctionReceiptProof struct {
	slot   int
	record dependencyRecord
}

func (m *managedFunctionMark) lockDependencySnapshot() func() {
	dependencies := rv64FunctionDependencyGraph()
	dependencies.mu.Lock()
	for i := range dependencies.records {
		record := &dependencies.records[i]
		if record.state != dependencyTyped || record.target == nil || record.consumer == nil {
			continue
		}
		target := m.graph.members[record.target.owner]
		consumer := m.graph.members[record.consumer.owner]
		if target == nil || consumer == nil || !dependencies.authoritativeLocked(record.target) || !dependencies.authoritativeLocked(record.consumer) {
			continue
		}
		m.incoming[target]++
		m.edges[consumer] = append(m.edges[consumer], target)
		m.receipts = append(m.receipts, managedFunctionReceiptProof{i, *record})
	}
	return dependencies.mu.Unlock
}
func (m *managedFunctionMark) lockDependencyCommit() (func(), bool) {
	dependencies := rv64FunctionDependencyGraph()
	dependencies.mu.Lock()
	for _, proof := range m.receipts {
		if proof.slot >= len(dependencies.records) || dependencies.records[proof.slot] != proof.record {
			dependencies.mu.Unlock()
			return nil, false
		}
	}
	return dependencies.mu.Unlock, true
}

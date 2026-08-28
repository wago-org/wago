package railmach

import (
	"fmt"
	"slices"
)

const LocationTemporary LocationKind = 0xff

type MoveKind uint8

const (
	MoveInvalid MoveKind = iota
	MoveCopy
	MoveSaveTemporary
	MoveRestoreTemporary
	MoveRematerialize
)

type MovePlacement uint8

const (
	PlaceInvalid MovePlacement = iota
	PlacePredecessorEnd
	PlaceSuccessorStart
	PlaceSplitEdge
	PlaceBeforeInstruction
)

type PhysicalMove struct {
	Src       Location
	Dst       Location
	Reg       VReg
	Edge      uint32
	Position  uint32
	Kind      MoveKind
	Placement MovePlacement
	Bank      Bank
	Temporary uint8
}

type MoveRange struct {
	Start uint32
	Count uint32
}

type CopyDebt struct {
	Requested      uint32
	Coalesced      uint32
	Physical       uint32
	Cycles         uint32
	SpillToSpill   uint32
	Rematerialized uint32
	Motion         uint32
}

type SSAExit struct {
	Moves       []PhysicalMove
	EdgeMoves   []MoveRange
	FixedMoves  []MoveRange
	FixedPoints []uint32
	Debt        CopyDebt

	fixedScratch []FixedMove
	pending      []pendingCopy
	predSuccs    []uint16
	succPreds    []uint16
}

type pendingCopy struct {
	src Location
	dst Location
	reg VReg
}

// LateSSAExit resolves allocated block arguments and fixed-register uses only
// after physical assignment. Parallel copies retain edge placement, break
// cycles with one preserved bank temporary, and expand spill-to-spill copies
// through a second transfer temporary.
func LateSSAExit(f *Func, allocation *Allocation, reuse *SSAExit) (*SSAExit, error) {
	if err := verifyAllocationReusingScratch(f, allocation, DefaultLinearQConfig(f.Target)); err != nil {
		return nil, err
	}
	return lateSSAExitVerifiedAllocation(f, allocation, reuse)
}

// LateSSAExitVerifiedAllocation resolves an allocation returned by this
// package's verified allocators without replaying the complete allocation
// verifier at the immediately adjacent boundary. The produced SSA exit is
// still independently verified before return.
func LateSSAExitVerifiedAllocation(f *Func, allocation *Allocation, reuse *SSAExit) (*SSAExit, error) {
	if f == nil || allocation == nil {
		return nil, fmt.Errorf("railmach: late SSA exit requires a verified allocation")
	}
	return lateSSAExitVerifiedAllocation(f, allocation, reuse)
}

func lateSSAExitVerifiedAllocation(f *Func, allocation *Allocation, reuse *SSAExit) (*SSAExit, error) {
	if reuse == nil {
		reuse = new(SSAExit)
	}
	moves := reuse.Moves[:0]
	edgeMoves := resize(reuse.EdgeMoves, len(f.Edges))
	fixedMoves := reuse.FixedMoves[:0]
	fixedPoints := reuse.FixedPoints[:0]
	fixedScratch := append(reuse.fixedScratch[:0], allocation.FixedMoves...)
	pending := reuse.pending[:0]
	predSuccs := resize(reuse.predSuccs, len(f.Blocks))
	succPreds := resize(reuse.succPreds, len(f.Blocks))
	clear(predSuccs)
	clear(succPreds)
	*reuse = SSAExit{Moves: moves, EdgeMoves: edgeMoves, FixedMoves: fixedMoves, FixedPoints: fixedPoints, fixedScratch: fixedScratch, pending: pending, predSuccs: predSuccs, succPreds: succPreds}

	for _, edge := range f.Edges {
		predSuccs[edge.From]++
		succPreds[edge.To]++
	}
	for edgeIndex := range f.Edges {
		start := uint32(len(reuse.Moves))
		pending = pending[:0]
		for _, transfer := range f.Transfers {
			if int(transfer.Edge) != edgeIndex {
				continue
			}
			reuse.Debt.Requested++
			src, dst := allocation.Locations[transfer.Src], allocation.Locations[transfer.Dst]
			if src == dst {
				reuse.Debt.Coalesced++
				continue
			}
			pending = append(pending, pendingCopy{src: src, dst: dst, reg: transfer.Src})
		}
		edge := f.Edges[edgeIndex]
		predLegal, succLegal := predSuccs[edge.From] == 1, succPreds[edge.To] == 1
		placement := PlaceSplitEdge
		switch {
		case predLegal && succLegal && f.Blocks[edge.To].Weight <= f.Blocks[edge.From].Weight:
			// Move the complete parallel bundle across the edge into a successor
			// that is no hotter. Keeping the bundle intact preserves cycle semantics.
			placement = PlaceSuccessorStart
		case predLegal:
			placement = PlacePredecessorEnd
		case succLegal:
			placement = PlaceSuccessorStart
		}
		if err := resolveParallel(&reuse.Moves, pending, uint32(edgeIndex), 0, placement, &reuse.Debt); err != nil {
			return nil, fmt.Errorf("railmach: edge %d SSA exit: %w", edgeIndex, err)
		}
		reuse.EdgeMoves[edgeIndex] = MoveRange{Start: start, Count: uint32(len(reuse.Moves)) - start}
		if predLegal && succLegal && placement == PlaceSuccessorStart {
			reuse.Debt.Motion += reuse.EdgeMoves[edgeIndex].Count
		}
	}

	// Fixed operands at one logical position form a parallel copy bundle.
	slices.SortFunc(reuse.fixedScratch, func(a, b FixedMove) int {
		if a.Position != b.Position {
			if a.Position < b.Position {
				return -1
			}
			return 1
		}
		if a.Bank != b.Bank {
			return int(a.Bank) - int(b.Bank)
		}
		return int(a.Reg) - int(b.Reg)
	})
	for first := 0; first < len(reuse.fixedScratch); {
		position, bank := reuse.fixedScratch[first].Position, reuse.fixedScratch[first].Bank
		last := first + 1
		for last < len(reuse.fixedScratch) && reuse.fixedScratch[last].Position == position && reuse.fixedScratch[last].Bank == bank {
			last++
		}
		start := uint32(len(reuse.Moves))
		pending = pending[:0]
		for _, fixed := range reuse.fixedScratch[first:last] {
			dst := Location{Kind: LocationRegister, Bank: fixed.Bank, Index: uint16(fixed.Physical)}
			pending = append(pending, pendingCopy{src: allocation.Locations[fixed.Reg], dst: dst, reg: fixed.Reg})
			reuse.Debt.Requested++
		}
		if err := resolveParallel(&reuse.Moves, pending, ^uint32(0), position, PlaceBeforeInstruction, &reuse.Debt); err != nil {
			return nil, fmt.Errorf("railmach: fixed bundle at %d: %w", position, err)
		}
		reuse.FixedPoints = append(reuse.FixedPoints, position)
		reuse.FixedMoves = append(reuse.FixedMoves, MoveRange{Start: start, Count: uint32(len(reuse.Moves)) - start})
		first = last
	}
	reuse.pending = pending
	if err := verifySSAExitReusingScratch(f, allocation, reuse); err != nil {
		return nil, err
	}
	return reuse, nil
}

func resolveParallel(out *[]PhysicalMove, copies []pendingCopy, edge, position uint32, placement MovePlacement, debt *CopyDebt) error {
	pending := copies
	for len(pending) != 0 {
		progress := false
		for index, copy := range pending {
			destinationIsSource := false
			for other, candidate := range pending {
				if other != index && candidate.src == copy.dst {
					destinationIsSource = true
					break
				}
			}
			if destinationIsSource {
				continue
			}
			emitResolvedMove(out, copy, edge, position, placement, debt)
			pending = append(pending[:index], pending[index+1:]...)
			progress = true
			break
		}
		if progress {
			continue
		}
		cycle := pending[0]
		if cycle.dst.Kind != LocationRegister && cycle.dst.Kind != LocationSpill {
			return fmt.Errorf("cannot break cycle through destination %#v", cycle.dst)
		}
		*out = append(*out, PhysicalMove{Src: cycle.dst, Reg: cycle.reg, Edge: edge, Position: position, Kind: MoveSaveTemporary, Placement: placement, Bank: cycle.dst.Bank})
		debt.Cycles++
		temporary := Location{Kind: LocationTemporary, Bank: cycle.dst.Bank}
		for index := range pending {
			if pending[index].src == cycle.dst {
				pending[index].src = temporary
			}
		}
	}
	return nil
}

func emitResolvedMove(out *[]PhysicalMove, copy pendingCopy, edge, position uint32, placement MovePlacement, debt *CopyDebt) {
	move := PhysicalMove{Src: copy.src, Dst: copy.dst, Reg: copy.reg, Edge: edge, Position: position, Kind: MoveCopy, Placement: placement, Bank: copy.dst.Bank}
	switch {
	case copy.src.Kind == LocationTemporary:
		move.Kind = MoveRestoreTemporary
		*out = append(*out, move)
	case copy.src.Kind == LocationRematerialize:
		move.Kind = MoveRematerialize
		*out = append(*out, move)
		debt.Rematerialized++
	case copy.src.Kind == LocationSpill && copy.dst.Kind == LocationSpill:
		*out = append(*out,
			PhysicalMove{Src: copy.src, Reg: copy.reg, Edge: edge, Position: position, Kind: MoveSaveTemporary, Placement: placement, Bank: copy.src.Bank, Temporary: 1},
			PhysicalMove{Dst: copy.dst, Reg: copy.reg, Edge: edge, Position: position, Kind: MoveRestoreTemporary, Placement: placement, Bank: copy.dst.Bank, Temporary: 1},
		)
		debt.SpillToSpill++
	default:
		*out = append(*out, move)
	}
	debt.Physical = uint32(len(*out))
}

func VerifySSAExit(f *Func, allocation *Allocation, exit *SSAExit) error {
	if f == nil {
		return fmt.Errorf("railmach: malformed SSA exit header")
	}
	return verifySSAExit(f, allocation, exit, make([]uint16, len(f.Blocks)), make([]uint16, len(f.Blocks)))
}

func verifySSAExitReusingScratch(f *Func, allocation *Allocation, exit *SSAExit) error {
	if f == nil || exit == nil {
		return fmt.Errorf("railmach: malformed SSA exit header")
	}
	predSuccs := resize(exit.predSuccs, len(f.Blocks))
	succPreds := resize(exit.succPreds, len(f.Blocks))
	clear(predSuccs)
	clear(succPreds)
	exit.predSuccs, exit.succPreds = predSuccs, succPreds
	return verifySSAExit(f, allocation, exit, predSuccs, succPreds)
}

func verifySSAExit(f *Func, allocation *Allocation, exit *SSAExit, predSuccs, succPreds []uint16) error {
	if f == nil || allocation == nil || exit == nil || len(exit.EdgeMoves) != len(f.Edges) || len(exit.FixedMoves) != len(exit.FixedPoints) {
		return fmt.Errorf("railmach: malformed SSA exit header")
	}
	for _, edge := range f.Edges {
		predSuccs[edge.From]++
		succPreds[edge.To]++
	}
	verifiedMotion := uint32(0)
	for edge, moves := range exit.EdgeMoves {
		if uint64(moves.Start)+uint64(moves.Count) > uint64(len(exit.Moves)) {
			return fmt.Errorf("railmach: edge %d move range is invalid", edge)
		}
		placement := PlaceInvalid
		for _, move := range exit.Moves[moves.Start : moves.Start+moves.Count] {
			if move.Edge != uint32(edge) || move.Placement < PlacePredecessorEnd || move.Placement > PlaceSplitEdge {
				return fmt.Errorf("railmach: edge %d has misplaced move %#v", edge, move)
			}
			if placement == PlaceInvalid {
				placement = move.Placement
			} else if move.Placement != placement {
				return fmt.Errorf("railmach: edge %d parallel bundle has mixed placement", edge)
			}
			cfgEdge := f.Edges[edge]
			if (move.Placement == PlacePredecessorEnd && predSuccs[cfgEdge.From] != 1) || (move.Placement == PlaceSuccessorStart && succPreds[cfgEdge.To] != 1) {
				return fmt.Errorf("railmach: edge %d move has illegal placement %#v", edge, move)
			}
			if move.Placement == PlaceSuccessorStart && predSuccs[cfgEdge.From] == 1 && succPreds[cfgEdge.To] == 1 && f.Blocks[cfgEdge.To].Weight <= f.Blocks[cfgEdge.From].Weight {
				verifiedMotion++
			}
		}
	}
	if verifiedMotion != exit.Debt.Motion {
		return fmt.Errorf("railmach: physical copy motion %d does not match debt %d", verifiedMotion, exit.Debt.Motion)
	}
	for id, moves := range exit.FixedMoves {
		if uint64(moves.Start)+uint64(moves.Count) > uint64(len(exit.Moves)) {
			return fmt.Errorf("railmach: fixed bundle %d move range is invalid", id)
		}
		for _, move := range exit.Moves[moves.Start : moves.Start+moves.Count] {
			if move.Placement != PlaceBeforeInstruction || move.Position != exit.FixedPoints[id] {
				return fmt.Errorf("railmach: fixed bundle %d has misplaced move %#v", id, move)
			}
		}
	}
	for id, move := range exit.Moves {
		if move.Kind == MoveInvalid || move.Bank == BankInvalid {
			return fmt.Errorf("railmach: physical move %d is invalid", id)
		}
		if move.Temporary > 1 {
			return fmt.Errorf("railmach: physical move %d has invalid temporary %d", id, move.Temporary)
		}
		if move.Kind == MoveCopy && (move.Src.Kind == LocationInvalid || move.Dst.Kind == LocationInvalid || move.Src == move.Dst) {
			return fmt.Errorf("railmach: physical copy %d is invalid", id)
		}
		if move.Kind == MoveRematerialize && (move.Reg == 0 || f.VRegs[move.Reg].Flags&VRegRematerializable == 0) {
			return fmt.Errorf("railmach: rematerialization %d is invalid", id)
		}
	}
	return nil
}

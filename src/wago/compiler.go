package wago

import (
	corecompiler "github.com/wago-org/wago/src/core/compiler"
	"github.com/wago-org/wago/src/core/compiler/backend/dragline"
)

// CompilerEngine identifies one complete compiler pipeline.
type CompilerEngine = corecompiler.Engine

// DraglineUnavailableError reports that strict Dragline compilation was
// selected before the independent optimizing pipeline can produce native code.
type DraglineUnavailableError = dragline.UnavailableError

const (
	CompilerRailshot = corecompiler.EngineRailshot
	CompilerDragline = corecompiler.EngineDragline
)

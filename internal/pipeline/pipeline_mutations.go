package pipeline

import (
	"github.com/blanergol/agent-core/pkg/stages"
)

// PipelineMutations возвращает полный пользовательский набор изменений пайплайна.
func PipelineMutations() []stages.PipelineMutation {
	return []stages.PipelineMutation{
		stages.InsertBefore("observe", stages.NewSanitizeStage(InputSanitizer{})),
		stages.InsertAfter("sanitize", NewInputGateStage()),
		stages.InsertAfter("reflect", NewPostReflectAuditStage()),
	}
}

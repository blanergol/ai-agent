package agent

// ContractChangeNote фиксирует минимальную запись об эволюции публичного контракта агента.
type ContractChangeNote struct {
	// Version указывает версию контракта, к которой относится изменение.
	Version string
	// Date хранит дату фиксации изменения в формате YYYY-MM-DD.
	Date string
	// Summary кратко описывает наблюдаемое внешнее изменение/уточнение поведения.
	Summary string
}

// ContractChangeNotes хранит журнал ключевых изменений контракта в коде.
// Записи должны быть обратносовместимыми в рамках одной APIVersion.
var ContractChangeNotes = []ContractChangeNote{
	{
		Version: "v1",
		Date:    "2026-03-03",
		Summary: "Hardened runtime contracts: tool retry/backoff policy, session-scoped dedup, graceful serve shutdown, user_sub_hash logging.",
	},
}

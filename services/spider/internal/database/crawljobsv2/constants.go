package crawljobsv2

const (
	ProtocolVersion = 2

	MaxExactInteger uint64 = 9_007_199_254_740_991

	LeaseTTLMilliseconds             uint64 = 60_000
	LeaseRenewalIntervalMilliseconds uint64 = 10_000
	MaxDeliveryAttempts              uint64 = 3
	RetryDelayAttempt1Milliseconds   uint64 = 30_000
	RetryDelayAttempt2Milliseconds   uint64 = 120_000
	MaxPreIOExpiredLeaseRecoveries   uint64 = 3

	MaxRequestStartsPerRun          uint64 = 10
	MaxReservationCreationsPerRun   uint64 = 100
	MinRequestStartsPerGroup        uint64 = 1
	MaxRequestStartsPerGroup        uint64 = 10
	GlobalActiveRequestLimit        uint64 = 2
	MaxScopeConcurrency             uint64 = 32
	MaxScopeIntervalMilliseconds    uint64 = 3_600_000
	GlobalScopeIntervalMilliseconds uint64 = 0
	MaxDurableRateScopes            uint64 = 100_000

	MaxPolicyGroupsPerRun      = 64
	MaxPolicyGroupIDBytes      = 128
	MaxRenderPolicyRuleIDBytes = 128
	MaxActiveLeases            = 64
	MaxJobsPerRun              = 10_000
	MaxActiveRuns              = 16
	MaxUnarchivedRuns          = 100
	MaxRunRecordsPendingPurge  = 128

	MaxPagesQueueDepthBeforeCommit = 5_000
	FeederEnqueueBatchSize         = 500
	RunAuditBatchSize              = 100
	ReadyMetadataPageSize          = 128
	MaintenanceBatchSize           = 100

	MaxOutlinksPerJob     = 256
	MaxDiscoveriesPerJob  = 128
	MaxImagesPerPage      = 64
	MaxAliasesPerJob      = 5
	MaxCanonicalURLBytes  = 2_048
	MaxPageBlobBytes      = 5 * 1024 * 1024
	MaxCombinedHTMLBytes  = 10 * 1024 * 1024
	MaxImageAltBytes      = 1_024
	MaxImageManifestBytes = 393_216

	StageTTLMilliseconds                            uint64 = 900_000
	MaxCommittedStageCleanupTTLMilliseconds         uint64 = 60_000
	MaxCommitBackpressureMilliseconds               uint64 = 120_000
	CommitBackpressureBeforeStageExpiryMilliseconds uint64 = 10_000
	MaxStageSlots                                          = 4
	StageMemoryReservationBytes                     uint64 = 50_331_648
	StageControlReservationBytes                    uint64 = 65_536
	TerminalStageControlFloorBytes                  uint64 = 32_768
	CommitMemoryReservationBytes                    uint64 = 67_108_864
	LeaseSafetyReservationBytes                     uint64 = 16_777_216
	MaxStageAggregateLogicalDataBytes               uint64 = 14_680_064
	MaxStageKeys                                           = 73
	ReservationTombstoneTTLSeconds                  uint64 = 86_400

	MaxAuthorizationWindowMilliseconds    uint64 = 24 * 60 * 60 * 1000
	MinAuthorizationRemainingMilliseconds uint64 = 60 * 1000
	MaxOrdinaryEvalSHARequestBytes               = 2 * 1024 * 1024
	MaxPageBlobEvalSHARequestBytes               = 5_373_952
	MaxCommitEvalSHARequestBytes                 = 64 * 1024
	MaxNonBlobStageBatchRecords                  = 64
	MaxNonBlobStageBatchRequestBytes             = 512 * 1024
	MaxOutlinkChunks                             = 4
	MaxDiscoveryChunks                           = 2
	FinalPageFieldCount                          = 10
	MaxResponseEnvelopeScalars                   = 9
)

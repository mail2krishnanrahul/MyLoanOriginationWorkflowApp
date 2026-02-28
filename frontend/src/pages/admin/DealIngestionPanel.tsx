import { useState, useCallback } from 'react';
import { api } from '@/api/client';
import { Upload, Play, RotateCcw, CheckCircle2, XCircle, Loader2 } from 'lucide-react';

function generateDealId(): string {
    const now = new Date();
    const date = now.toISOString().slice(0, 10).replace(/-/g, '');
    const seq = String(Math.floor(Math.random() * 999999)).padStart(6, '0');
    return `DL-${date}-${seq}`;
}

function generateIdempotencyKey(dealId: string): string {
    return `IDEMP-${dealId}-${Date.now()}`;
}

function buildDefaultPayload(): string {
    const dealId = generateDealId();
    const now = new Date().toISOString();

    const request = {
        snapshot: {
            dealId,
            dealName: "Harbour Group Expansion - FY26",
            status: "DOCUMENT_VERIFICATION",
            banker: {
                bankerId: "BKR-10933",
                channel: "COMMERCIAL",
                portfolio: "Sydney CBD Commercial"
            },
            customerReference: "CRM-GROUP-771233",
            umbrellaLimit: {
                umbrellaLimitId: "UMB-77821",
                approvedLimit: { amount: 5000000.0, currency: "AUD" },
                currentUtilisation: { amount: 0.0, currency: "AUD" },
                availableHeadroom: { amount: 5000000.0, currency: "AUD" },
                exposureMethod: "GROSS",
                lastReconciledAt: now
            },
            legalParties: [
                {
                    partyId: "PTY-001",
                    partyType: "COMPANY",
                    legalName: "Harbour Trading Pty Ltd",
                    companyType: "PTY_LTD",
                    regulatory: { abn: "11 222 333 444", acn: "222333444", tfnStatus: "NOT_PROVIDED", gstRegistered: true },
                    contact: {
                        email: "accounts@harbourtrading.example.au",
                        phone: "+61-2-9000-1111",
                        address: { line1: "Level 10, 200 George St", suburb: "Sydney", state: "NSW", postcode: "2000", country: "AU" }
                    },
                    roles: ["OTHER"]
                },
                {
                    partyId: "PTY-002",
                    partyType: "COMPANY",
                    legalName: "Harbour Logistics Pty Ltd",
                    companyType: "PTY_LTD",
                    regulatory: { abn: "55 666 777 888", acn: "666777888", tfnStatus: "NOT_PROVIDED", gstRegistered: true },
                    contact: {
                        email: "finance@harbourlogistics.example.au",
                        phone: "+61-2-9000-2222",
                        address: { line1: "12-18 Port Access Rd", suburb: "Botany", state: "NSW", postcode: "2019", country: "AU" }
                    },
                    roles: ["SECURITY_PROVIDER"]
                }
            ],
            trustStructures: [
                {
                    trustId: "TRUST-001",
                    trustName: "The Harbour Family Trust",
                    trustType: "FAMILY",
                    trustDeedDate: "2014-06-30",
                    trustees: [{ partyId: "PTY-002", trusteeType: "CORPORATE_TRUSTEE", capacityStatement: "Harbour Logistics Pty Ltd ATF The Harbour Family Trust" }],
                    appointor: "Harbour Family Holdings (internal note)",
                    regulatory: { tfnStatus: "PROVIDED" }
                }
            ],
            borrowingEntities: [
                {
                    borrowingEntityId: "BE-001",
                    borrowingEntityType: "DIRECT",
                    displayName: "Harbour Trading Pty Ltd (in its own right)",
                    obligors: [{ partyId: "PTY-001", liabilityType: "JOINT_AND_SEVERAL", capacity: "IN_OWN_RIGHT" }],
                    facilities: [
                        {
                            facilityId: "FAC-001",
                            product: "COMMERCIAL_PROPERTY_FINANCE",
                            status: "DRAFT",
                            purpose: "Acquire owner-occupied warehouse (Alexandria NSW) and fit-out.",
                            facilityLimit: { amount: 3500000.0, currency: "AUD" },
                            umbrellaConsumption: { consumptionAmount: { amount: 3500000.0, currency: "AUD" }, rationale: "Gross exposure counted at facility limit." },
                            pricing: { rateType: "VARIABLE", interestRate: 0.0725, margin: 0.021, index: "BBSY", fees: [{ feeType: "ESTABLISHMENT", amount: { amount: 15000.0, currency: "AUD" } }] },
                            term: { startDate: "2026-03-15", endDate: "2036-03-15", termMonths: 120 },
                            repayment: { repaymentType: "PRINCIPAL_AND_INTEREST", frequency: "MONTHLY", amortisationMonths: 240 },
                            guarantees: [{ guaranteeType: "RELATED_ENTITY_GUARANTEE", guarantor: { guarantorType: "EXISTING_PARTY", partyId: "PTY-002", relationshipToBorrower: "RELATED_ENTITY" }, scope: "ALL_MONEYS", notes: "Related entity support as per credit policy." }],
                            settledAt: null,
                            closedAt: null
                        },
                        {
                            facilityId: "FAC-002",
                            product: "BUSINESS_ONE_OVERDRAFT",
                            status: "DRAFT",
                            purpose: "Working capital seasonal buffer for import cycles.",
                            facilityLimit: { amount: 1000000.0, currency: "AUD" },
                            umbrellaConsumption: { consumptionAmount: { amount: 1000000.0, currency: "AUD" }, rationale: "Revolving facility consumed at approved limit." },
                            pricing: { rateType: "VARIABLE", interestRate: 0.084, margin: 0.03, index: "CASH_RATE", fees: [{ feeType: "LINE_FEE", amount: { amount: 6000.0, currency: "AUD" } }] },
                            term: { startDate: "2026-03-15", endDate: "2029-03-15", termMonths: 36 },
                            repayment: { repaymentType: "REVOLVING", frequency: "MONTHLY", amortisationMonths: 0 },
                            guarantees: [{ guaranteeType: "RELATED_ENTITY_GUARANTEE", guarantor: { guarantorType: "EXISTING_PARTY", partyId: "PTY-002", relationshipToBorrower: "RELATED_ENTITY" }, scope: "ALL_MONEYS", notes: "Cross-support from related entity for working capital." }],
                            settledAt: null,
                            closedAt: null
                        }
                    ]
                },
                {
                    borrowingEntityId: "BE-TRUST-001",
                    borrowingEntityType: "TRUST",
                    trustId: "TRUST-001",
                    displayName: "Harbour Logistics Pty Ltd ATF The Harbour Family Trust",
                    obligors: [{ partyId: "PTY-002", liabilityType: "JOINT_AND_SEVERAL", capacity: "AS_TRUSTEE", capacityStatement: "Harbour Logistics Pty Ltd ATF The Harbour Family Trust" }],
                    facilities: [
                        {
                            facilityId: "FAC-003",
                            product: "EQUIPMENT_FINANCE",
                            status: "DRAFT",
                            purpose: "Fleet upgrade: 6 delivery vehicles.",
                            facilityLimit: { amount: 500000.0, currency: "AUD" },
                            umbrellaConsumption: { consumptionAmount: { amount: 500000.0, currency: "AUD" }, rationale: "Gross exposure at facility limit." },
                            pricing: { rateType: "FIXED", interestRate: 0.069, margin: 0.0, index: "OTHER", fees: [{ feeType: "ESTABLISHMENT", amount: { amount: 3500.0, currency: "AUD" } }] },
                            term: { startDate: "2026-03-15", endDate: "2031-03-15", termMonths: 60 },
                            repayment: { repaymentType: "PRINCIPAL_AND_INTEREST", frequency: "MONTHLY", amortisationMonths: 60 },
                            guarantees: [{ guaranteeType: "RELATED_ENTITY_GUARANTEE", guarantor: { guarantorType: "EXISTING_PARTY", partyId: "PTY-001", relationshipToBorrower: "RELATED_ENTITY" }, scope: "LIMITED", limitedAmount: { amount: 250000.0, currency: "AUD" }, notes: "Limited cross-support for equipment facility." }],
                            settledAt: null,
                            closedAt: null
                        }
                    ]
                }
            ],
            security: {
                secured: true,
                unsecuredReason: null,
                collaterals: [
                    {
                        collateralId: "COL-001",
                        collateralType: "REGISTERED_MORTGAGE",
                        securityProviderPartyIds: ["PTY-002"],
                        ranking: "FIRST",
                        assets: [
                            { assetId: "AST-PR-001", assetType: "PROPERTY", propertyType: "COMMERCIAL", address: { line1: "45 Industrial Ave", suburb: "Alexandria", state: "NSW", postcode: "2015", country: "AU" }, titleReference: "DP123456 Lot 7", occupancyStatus: "OWNER_OCCUPIED", valuation: { value: { amount: 6200000.0, currency: "AUD" }, valuationDate: "2026-02-01", basis: "MARKET", valuer: "Independent Valuers Pty Ltd" } },
                            { assetId: "AST-GSA-001", assetType: "GSA", securedParty: "WESTPAC", coverage: "ALL_PRESENT_AND_AFTER_ACQUIRED_PROPERTY", ppsrRegistrationNumber: "PPSR-NSW-88990011", valuation: { value: { amount: 0.0, currency: "AUD" }, valuationDate: "2026-02-10", basis: "BANK", valuer: "N/A" } }
                        ]
                    }
                ],
                facilityAllocations: [
                    { borrowingEntityId: "BE-001", facilityId: "FAC-001", collateralId: "COL-001", allocatedAmount: { amount: 3500000.0, currency: "AUD" }, priority: 1, notes: "Mortgage package supports Commercial Property Finance." },
                    { borrowingEntityId: "BE-001", facilityId: "FAC-002", collateralId: "COL-001", allocatedAmount: { amount: 1000000.0, currency: "AUD" }, priority: 2, notes: "Same collateral package supports Business One Overdraft under umbrella." },
                    { borrowingEntityId: "BE-TRUST-001", facilityId: "FAC-003", collateralId: "COL-001", allocatedAmount: { amount: 500000.0, currency: "AUD" }, priority: 3, notes: "GSA component within package supports Equipment Finance exposure." }
                ]
            },
            lifecycleHistory: [{ operationId: "OP-0001", type: "NEW_MONEY", requestedAt: now, requestedBy: "BKR-10933", status: "RECEIVED", summary: "Initial creation of deal and proposed facilities under umbrella." }],
            notes: "Proposed structure: Commercial Property Finance + Business One Overdraft + Equipment Finance under umbrella, secured by mortgage package.",
            createdAt: now,
            updatedAt: now
        },
        snapshotTimestamp: now,
        sourceSystem: "MANUAL_OVERRIDE",
        correlationId: `CORR-${dealId}`
    };

    return JSON.stringify(request, null, 2);
}

interface IngestionResult {
    success: boolean;
    status: number;
    data: Record<string, unknown>;
}

export function DealIngestionPanel() {
    const [payload, setPayload] = useState<string>(buildDefaultPayload);
    const [isSubmitting, setIsSubmitting] = useState(false);
    const [result, setResult] = useState<IngestionResult | null>(null);
    const [jsonError, setJsonError] = useState<string | null>(null);

    const handleReset = useCallback(() => {
        setPayload(buildDefaultPayload());
        setResult(null);
        setJsonError(null);
    }, []);

    const handleValidate = useCallback(() => {
        try {
            JSON.parse(payload);
            setJsonError(null);
            return true;
        } catch (e) {
            setJsonError((e as Error).message);
            return false;
        }
    }, [payload]);

    const handleSubmit = useCallback(async () => {
        if (!handleValidate()) return;

        setIsSubmitting(true);
        setResult(null);

        try {
            const parsed = JSON.parse(payload);
            const dealId = parsed.snapshot?.dealId || 'unknown';
            const idempotencyKey = generateIdempotencyKey(dealId);

            const response = await api.post('/ingest/deals', parsed, {
                headers: {
                    'X-Idempotency-Key': idempotencyKey,
                },
                validateStatus: () => true,
            });

            setResult({
                success: response.status >= 200 && response.status < 300,
                status: response.status,
                data: response.data,
            });
        } catch (err: unknown) {
            const message = err instanceof Error ? err.message : 'Unknown error';
            setResult({
                success: false,
                status: 0,
                data: { error: message },
            });
        } finally {
            setIsSubmitting(false);
        }
    }, [payload, handleValidate]);

    return (
        <div className="rounded-xl border border-neutral-200 bg-white shadow-sm">
            <div className="flex items-center justify-between border-b border-neutral-100 px-6 py-4">
                <div>
                    <h2 className="text-lg font-semibold flex items-center gap-2">
                        <Upload className="w-5 h-5" /> Deal Ingestion
                    </h2>
                    <p className="text-sm text-neutral-500 mt-0.5">
                        Submit a deal snapshot to create a new Document Verification case with auto-generated tags.
                    </p>
                </div>
                <div className="flex gap-2">
                    <button
                        type="button"
                        onClick={handleReset}
                        className="inline-flex items-center gap-1.5 rounded-lg border border-neutral-300 bg-white px-3 py-1.5 text-sm font-medium text-neutral-700 hover:bg-neutral-50 transition-colors"
                    >
                        <RotateCcw size={14} /> Reset
                    </button>
                    <button
                        type="button"
                        onClick={handleSubmit}
                        disabled={isSubmitting}
                        className="inline-flex items-center gap-1.5 rounded-lg bg-blue-600 px-4 py-1.5 text-sm font-medium text-white hover:bg-blue-700 disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
                    >
                        {isSubmitting ? <Loader2 size={14} className="animate-spin" /> : <Play size={14} />}
                        {isSubmitting ? 'Ingesting...' : 'Ingest Deal'}
                    </button>
                </div>
            </div>

            <div className="p-6">
                <div className="mb-2 flex items-center justify-between">
                    <label className="text-sm font-medium text-neutral-700">
                        Request Payload (JSON)
                    </label>
                    {jsonError && (
                        <span className="text-xs text-red-600 flex items-center gap-1">
                            <XCircle size={12} /> {jsonError}
                        </span>
                    )}
                </div>
                <textarea
                    value={payload}
                    onChange={(e) => {
                        setPayload(e.target.value);
                        if (jsonError) setJsonError(null);
                    }}
                    spellCheck={false}
                    className="w-full h-96 font-mono text-xs leading-relaxed rounded-lg border border-neutral-300 bg-neutral-50 p-4 focus:border-blue-500 focus:ring-2 focus:ring-blue-500/20 focus:outline-none resize-y"
                />

                <p className="mt-2 text-xs text-neutral-400">
                    The deal status must be <code className="bg-neutral-100 px-1 py-0.5 rounded">DOCUMENT_VERIFICATION</code> to trigger case creation. Each submission generates a unique deal ID and idempotency key.
                </p>
            </div>

            {result && (
                <div className="border-t border-neutral-100 px-6 py-4">
                    <div className={`rounded-lg p-4 ${result.success ? 'bg-green-50 border border-green-200' : 'bg-red-50 border border-red-200'}`}>
                        <div className="flex items-center gap-2 mb-2">
                            {result.success ? (
                                <CheckCircle2 className="w-5 h-5 text-green-600" />
                            ) : (
                                <XCircle className="w-5 h-5 text-red-600" />
                            )}
                            <span className={`text-sm font-semibold ${result.success ? 'text-green-800' : 'text-red-800'}`}>
                                {result.success ? 'Deal ingested successfully' : 'Ingestion failed'} (HTTP {result.status})
                            </span>
                        </div>
                        <pre className="text-xs font-mono whitespace-pre-wrap overflow-auto max-h-48 text-neutral-700">
                            {JSON.stringify(result.data, null, 2)}
                        </pre>
                        {result.success && result.data?.caseAction != null && (
                            <p className="mt-2 text-sm text-green-700">
                                {'Case action: '}
                                <strong>{String(result.data.caseAction)}</strong>
                                {result.data.caseId != null && (
                                    <span>{' — Case ID: '}<code className="bg-green-100 px-1 rounded">{String(result.data.caseId)}</code></span>
                                )}
                            </p>
                        )}
                    </div>
                </div>
            )}
        </div>
    );
}

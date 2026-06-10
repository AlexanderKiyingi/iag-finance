# Payroll ↔ ERP boundary

Workforce master data and leave workflow live in **iag-erp**. Payroll journal runs and payslips live in **iag-finance**. The boundary is **Kafka on `iag.operations`**, not shared HTTP APIs.

## Responsibility split

| Concern | Owner | Mechanism |
|---------|-------|-----------|
| Employee roster, leave requests, attendance | **iag-erp** | REST `/api/v1/erp/...` |
| Payroll employee mirror + leave accrual queue | **iag-finance** | Consumes `erp.employee.*`, `erp.leave.*` |
| GL journal posting (payroll run) | **iag-finance** | Manual or future `POST /v1/ledger/entries` workflow |

## Events consumed by finance

Consumer group: `iag.finance.erp` on topic **`iag.operations`**.

| Event | Finance action |
|-------|----------------|
| `erp.employee.created` | Upsert `payroll_employee_refs` |
| `erp.employee.updated` | Upsert mirror row |
| `erp.employee.terminated` | Set status `terminated` |
| `erp.leave.approved` | Insert `payroll_leave_accruals` (`accrual_status=approved`) |
| `erp.leave.rejected` | Insert accrual row (`rejected`) |
| `erp.leave.cancelled` | Insert accrual row (`cancelled`) |

Idempotency: `source_event_id` is unique per Kafka envelope id.

## Read APIs (payroll prep UI)

| Method | Path | Permission |
|--------|------|------------|
| GET | `/v1/payroll/employees?status=` | `finance.view_operations` |
| GET | `/v1/payroll/leave-accruals?employee_no=&status=` | `finance.view_operations` |

Gateway prefix: `/api/v1/finance/v1/payroll/...`

## Configuration

```env
ENABLE_CONSUMER=true
KAFKA_BROKERS=redpanda:9092
KAFKA_OPERATIONS_TOPIC=iag.operations
```

## Out of scope (finance)

- Payslip PDF generation
- Tax withholding calculation
- Direct ERP HTTP calls for HR CRUD

ERP publishes; finance mirrors. Journal posting from approved accruals is a separate finance workflow.

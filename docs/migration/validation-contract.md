# Legacy Migration Validation Contract

本文件定義production rehearsal與cutover的blocking validation。Development/UAT可以產生非blocking report，但不得沿用「validation pass」名稱宣稱production complete。

## Completeness Equations

每一個source entity與stable subgroup都必須滿足：

```text
source_count = mapped_count + approved_skipped_count
unresolved_skipped_count = 0
duplicate_target_count = 0
invalid_target_reference_count = 0
```

所有approved skip必須具有stable reason code、source identifier hash或private reference，以及核准依據。Manual follow-up不等於approved skip。

## Blocking Gates

### Structural

- Source files、schema/version與checksum和immutable run manifest一致。
- Migration order、tool version與target schema migration version已記錄。
- Target foreign keys、required fields、mapping uniqueness與entity counts有效。
- Full run後rerun不產生非預期insert/update，rollback/failure evidence可重現。

### Leasing And Billing

- 不存在未核准的overlapping active leases。
- Rent/electricity cadence分布與source/mapping一致。
- Starting meter與actual move-out保存數量可以reconcile。
- Generated bill periods不重複，且每筆都有明確source/mapping basis。
- 每個paid/overdue/written-off狀態都有已核准的source evidence與mapping rule。

### Accounting

- Property/month/category收入支出可對帳。
- Rent、electricity、deposit、refund、deduction與checkout totals有獨立equation。
- Journal expense/accounting title mapping完整，unknown title有approved handling。

### Identity And Access

- Active owner/staff identity可登入且property scope正確。
- Placeholder identity為零，或每筆都有明確production核准。
- Owner/member/property assignment source均納入completeness equation。

### Attachments

- Metadata resource mapping完整。
- 每個需複製object的source checksum/size、target existence/checksum與result已記錄。
- Missing、excluded與redacted objects分別使用approved reason code；不得抓取或提交整包未篩選uploads。

### Application Sign-Off

- 每個property有代表性的authorized API/read-model spot checks。
- Financial、access、attachment與checkout結果經business reviewer sign-off。
- Blocking gates全過後才能產生production sign-off；單一Task/stage report不能替代整體證據。

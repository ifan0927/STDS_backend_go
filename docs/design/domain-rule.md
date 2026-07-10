# Domain Rules

本文件只收錄已確認、足以約束 schema 與 API 設計的 business rules。它不記錄 implementation status、issue、transport shape、schema欄位或未來假設。現行實作若不同，差異應記入 [`decision-backlog.md`](decision-backlog.md)，不得直接改寫本文件。

## Shared Rules

### DR-S01 Authorization Scope

系統以角色決定可使用的功能，並以物業關聯限制可存取的資源。業主只能讀取自己名下物業的允許資訊；工作室成員只能存取其被授權的物業，系統管理員的例外不得跳過資源識別與不存在檢查。

### DR-S02 Financial Atomicity

會改變付款、押金、結算或會計分錄的單一業務操作，其核心狀態與對應會計效果必須同時成功或同時失敗，不得依賴無持久保證的 post-commit side effect 補寫。

### DR-S03 Historical Report Facts

已完成的財務快照與退租結算必須保存當時使用的金額與顯示事實；重印或查閱歷史結果時，不得用後續已變更的租客、房間、租約或科目資料重新解釋。

## Identity And Access

### DR-I01 Property Assignment Eligibility

物業工作指派只適用於工作室成員，不得把業主當作工作人員指派。

### DR-I02 Administrator Self-Protection

系統管理員不得透過自助操作降低自己的管理角色。

## Property

### DR-P01 Property And Room Deletion

仍有出租中房間的物業不得刪除。出租中或維修中的房間不得刪除。

### DR-P02 Room Availability For A New Lease

建立租約時，房間必須仍為空置；系統必須在可防止並行重複出租的鎖定邊界內再次確認。

### DR-P03 Electricity Price

物業電價必須大於零，可包含小數；只有系統管理員或主辦可修改。

## Leasing

### DR-L01 Lease Inputs

租約起始日不得晚於契約終止日，租金必須大於零，押金不得為負。

### DR-L02 Independent Billing Cadences

租金與電費的計費週期是兩個獨立租約條件，必須分別驗證與產生期次。物業預設電費週期只影響新租約；既有租約不得以一般編輯改變任一計費週期。

### DR-L03 Rent Period Semantics

租金付款日是租金期次起日。租金期次以租約起始日為基準，依約定週期推進；最後不足完整週期的短期次仍收完整期次租金，第一版不自動按日比例計算。

### DR-L04 Normal Termination

正常終止租約前，不能保留尚未處理的帳單；押金有扣款時必須記錄原因。強制終止適用不同流程，但其可 write off 範圍仍待確認，不能由本規則推定。

### DR-L05 Lease Replacement Scope

租約條件重建只適用於同一租客、同一房間與同一物業，且只能在租金與電費的完整計費邊界進行。邊界前帳單必須處理完畢，第一版押金只可延續到新租約。

### DR-L06 Checkout Preview And Finalization

退租預覽由後端計算且不得改變業務狀態。正式結算必須以未過期且內容相符的預覽結果為基礎，在同一交易內重新計算、保存不可變結算事實、處理押金與會計效果並終止租約。

## Billing

### DR-B01 Meter Reading

本期電表讀數不得小於前期讀數。電費由系統以用量乘抄表時適用的單價計算，最終台幣帳單金額四捨五入為整數，不由操作人員直接輸入。

### DR-B02 Payment

帳單不得重複收款；收款金額必須等於帳單金額，第一版不支援部分收款或溢收。逾期帳單仍可正常收款。

## Attachments

### DR-A01 Upload Limits

可上傳格式限 JPEG、PNG、HEIC 與 PDF，單檔上限 20 MB。產生上傳授權與正式登記時都必須驗證；保存期限、身分證件與刪除政策另待確認。

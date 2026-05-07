CREATE TABLE IF NOT EXISTS accounting_titles (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code VARCHAR(20) NOT NULL UNIQUE,
    name VARCHAR(100) NOT NULL,
    kind VARCHAR(20) NOT NULL CHECK (kind IN ('income', 'expense')),
    legacy_kind TEXT,
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS legacy_accounting_title_mappings (
    legacy_accounting_id VARCHAR(50) PRIMARY KEY,
    legacy_accounting_title_id VARCHAR(50) NOT NULL UNIQUE,
    accounting_title_id UUID NOT NULL REFERENCES accounting_titles(id),
    legacy_accounting_kind TEXT NOT NULL,
    legacy_accounting_title TEXT NOT NULL,
    source_table VARCHAR(50) NOT NULL DEFAULT 'xx_accounting_title',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

WITH legacy_titles(legacy_accounting_id, legacy_kind, code, name) AS (
    VALUES
    ('13', '營業費用（成本）類', '6125', '勞工保險費'),
    ('12', '營業費用（成本）類', '6123', '伙食餐飲費'),
    ('11', '營業費用（成本）類', '6121', '職工訓練'),
    ('10', '營業費用（成本）類', '6115', '維護修繕費'),
    ('9', '營業費用（成本）類', '6111', '水電瓦斯費'),
    ('8', '營業費用（成本）類', '6108', '謄本書圖費'),
    ('7', '營業費用（成本）類', '6107', '電話費'),
    ('6', '營業費用（成本）類', '6106', '郵運費'),
    ('5', '營業費用（成本）類', '6105', '差旅(油)費'),
    ('4', '營業費用（成本）類', '6104', '設備(耐久財)'),
    ('3', '營業費用（成本）類', '6103', '文具用品'),
    ('2', '營業費用（成本）類', '6102', '庶務用品'),
    ('1', '營業費用（成本）類', '6101', '薪資支出'),
    ('14', '營業費用（成本）類', '6131', '捐贈費'),
    ('15', '營業費用（成本）類', '6133', '廣告費'),
    ('16', '營業費用（成本）類', '6135', '交際費'),
    ('17', '營業費用（成本）類', '6141', '稅捐支出'),
    ('18', '營業費用（成本）類', '6143', '利息支出'),
    ('19', '營業費用（成本）類', '6145', '佣金支出'),
    ('20', '營業費用（成本）類', '6190', '其他費用'),
    ('21', '營業收益類', '4101', '代辦案件收入'),
    ('22', '營業收益類', '4102', '樓管收入'),
    ('23', '營業收益類', '4103', '勞務收入'),
    ('24', '營業收益類', '4104', '租賃收入'),
    ('25', '營業收益類', '4105', '佣金收入'),
    ('26', '營業收益類', '4106', '其他收入'),
    ('27', '營業收益類', '4109', '折讓(減項)'),
    ('28', '營業收益類', '4601', '押金收入(暫收款)'),
    ('29', '營業收益類', '4603', '租金收入'),
    ('30', '營業收益類', '4605', '房客電費收入'),
    ('31', '營業費用（成本）類', '6688', '業主收益報酬'),
    ('32', '營業費用（成本）類', '6611', '電費'),
    ('33', '營業費用（成本）類', '6612', '自來水費'),
    ('34', '營業費用（成本）類', '6607', '電話(網路)費'),
    ('35', '營業費用（成本）類', '6645', '佣金支出'),
    ('36', '營業費用（成本）類', '6615', '維護修繕費'),
    ('37', '營業費用（成本）類', '6602', '庶務用品'),
    ('38', '營業費用（成本）類', '6601', '薪資支出'),
    ('39', '營業費用（成本）類', '6641', '稅捐支出'),
    ('40', '營業費用（成本）類', '6603', '文具用品'),
    ('41', '營業費用（成本）類', '6604', '設備支出'),
    ('42', '營業費用（成本）類', '6633', '廣告費'),
    ('43', '營業費用（成本）類', '6613', '清潔費用'),
    ('45', '營業收益類', '4602', '押金退回(減項)'),
    ('46', '營業收益類', '4604', '租金退回(減項)'),
    ('47', '營業費用（成本）類', '6126', '全民健保費'),
    ('48', '營業費用（成本）類', '6127', '勞退準備金'),
    ('49', '營業費用（成本）類', '6609', '有線電視費用'),
    ('50', '營業收益類', '4606', '其他收入'),
    ('51', '營業費用（成本）類', '6610', '管理費'),
    ('52', '營業費用（成本）類', '6681', '其他支出'),
    ('53', '營業收益類', '4607', '管理費收入'),
    ('54', '營業收益類', '4201', '契約簽訂'),
    ('55', '營業收益類', '4202', '申報實價登錄'),
    ('56', '營業收益類', '4211', '鑑界'),
    ('57', '營業收益類', '4213', '建物第一次登記'),
    ('58', '營業收益類', '4221', '土地買賣(贈與)登記'),
    ('59', '營業收益類', '4222', '建物買賣(贈與)登記'),
    ('60', '營業收益類', '4227', '繼承登記'),
    ('61', '營業收益類', '4225', '信託登記'),
    ('62', '營業收益類', '4231', '抵押權設定登記'),
    ('63', '營業收益類', '4232', '抵押權塗銷登記'),
    ('64', '營業收益類', '4241', '分割登記'),
    ('65', '營業收益類', '4242', '合併登記'),
    ('66', '營業收益類', '4243', '共有物分割登記'),
    ('67', '營業收益類', '4203', '代撰存證信函'),
    ('68', '營業收益類', '4281', '申報贈與稅'),
    ('69', '營業收益類', '4282', '申報遺產稅'),
    ('70', '營業收益類', '4283', '申報房地合一稅'),
    ('71', '營業收益類', '4286', '申請農業用地證明'),
    ('72', '營業費用（成本）類', '6201', '地政(相關)規費'),
    ('73', '營業費用（成本）類', '6202', '戶政規費'),
    ('74', '營業費用（成本）類', '6203', '行政規費'),
    ('75', '營業費用（成本）類', '6204', '司法規費'),
    ('76', '營業費用（成本）類', '6212', '買賣(書狀)規費'),
    ('77', '營業費用（成本）類', '6211', '複丈測量費'),
    ('78', '營業費用（成本）類', '6221', '印花稅'),
    ('79', '營業費用（成本）類', '6222', '土地增值稅'),
    ('80', '營業費用（成本）類', '6223', '建物契稅'),
    ('81', '營業費用（成本）類', '6224', '地價稅'),
    ('82', '營業費用（成本）類', '6225', '房屋稅'),
    ('83', '營業費用（成本）類', '6226', '贈與稅'),
    ('84', '營業費用（成本）類', '6227', '遺產稅'),
    ('85', '營業費用（成本）類', '6205', '公證費'),
    ('86', '營業費用（成本）類', '6231', '水費(分算)'),
    ('87', '營業費用（成本）類', '6232', '電費(分算)'),
    ('88', '營業費用（成本）類', '6233', '瓦斯費(分算)'),
    ('89', '營業費用（成本）類', '6234', '管理費(分算)'),
    ('90', '營業費用（成本）類', '6206', '履約保證費'),
    ('91', '營業費用（成本）類', '6241', '木頭便章'),
    ('92', '營業費用（成本）類', '6242', '郵資'),
    ('93', '營業費用（成本）類', '6249', '車馬費'),
    ('94', '營業費用（成本）類', '6213', '抵押(書狀)規費'),
    ('107', '營業費用（成本）類', '6228', '地價稅(分算)'),
    ('96', '營業收益類', '4121', '上德(國泰活存)'),
    ('97', '營業收益類', '4122', 'STDS(陽信活存)'),
    ('98', '營業收益類', '4123', 'First(秀玉郵局)'),
    ('99', '營業收益類', '4125', '零用金(亭君)'),
    ('100', '營業收益類', '4126', '零用金(奕男)'),
    ('101', '營業收益類', '4127', '零用金(怡文)'),
    ('104', '營業收益類', '4295', '預收款(減項)'),
    ('103', '營業收益類', '4212', '鑑界(協助釘樁)'),
    ('105', '營業收益類', '4210', '專業顧問(諮詢)費'),
    ('106', '營業收益類', '4200', '代辦費'),
    ('108', '營業費用（成本）類', '6229', '房屋稅(分算)'),
    ('109', '營業收益類', '4214', '房屋稅籍設立'),
    ('110', '營業費用（成本）類', '6243', '影印(藍晒圖)費'),
    ('111', '營業費用（成本）類', '6244', '建物成果圖轉繪'),
    ('112', '營業費用（成本）類', '6219', '土地登記(書狀)費'),
    ('113', '營業費用（成本）類', '6191', '零用金互轉'),
    ('114', '營業收益類', '4290', '其他收入'),
    ('115', '營業費用（成本）類', '6251', '其他支出'),
    ('116', '營業費用（成本）類', '6112', '房租管理費'),
    ('117', '營業收益類', '4284', '申報自用住宅增值稅'),
    ('118', '營業收益類', '4135', '信用卡(亭君)'),
    ('119', '營業收益類', '4136', '信用卡(怡文)'),
    ('120', '營業收益類', '4137', '信用卡(奕男)'),
    ('121', '營業費用（成本）類', '6122', '職工福利'),
    ('122', '營業費用（成本）類', '6109', '網路(雲端)費用'),
    ('123', '營業收益類', '4223', '車位異動(移轉)登記')
),
upserted_titles AS (
    INSERT INTO accounting_titles (code, name, kind, legacy_kind, is_active)
    SELECT
        code,
        name,
        CASE WHEN legacy_kind = '營業收益類' THEN 'income' ELSE 'expense' END,
        legacy_kind,
        true
    FROM legacy_titles
    ON CONFLICT (code) DO UPDATE SET
        name = EXCLUDED.name,
        kind = EXCLUDED.kind,
        legacy_kind = EXCLUDED.legacy_kind,
        is_active = true,
        updated_at = now()
    RETURNING id, code
)
INSERT INTO legacy_accounting_title_mappings (
    legacy_accounting_id,
    legacy_accounting_title_id,
    accounting_title_id,
    legacy_accounting_kind,
    legacy_accounting_title,
    source_table
)
SELECT
    lt.legacy_accounting_id,
    lt.code,
    at.id,
    lt.legacy_kind,
    lt.name,
    'xx_accounting_title'
FROM legacy_titles lt
JOIN upserted_titles at ON at.code = lt.code
ON CONFLICT (legacy_accounting_id) DO UPDATE SET
    legacy_accounting_title_id = EXCLUDED.legacy_accounting_title_id,
    accounting_title_id = EXCLUDED.accounting_title_id,
    legacy_accounting_kind = EXCLUDED.legacy_accounting_kind,
    legacy_accounting_title = EXCLUDED.legacy_accounting_title;

ALTER TABLE accounting_entries
    ADD COLUMN IF NOT EXISTS accounting_title_id UUID REFERENCES accounting_titles(id),
    ADD COLUMN IF NOT EXISTS accounting_title_code VARCHAR(20),
    ADD COLUMN IF NOT EXISTS accounting_title_name VARCHAR(100);

ALTER TABLE monthly_snapshot_entries
    ADD COLUMN IF NOT EXISTS accounting_title_id UUID REFERENCES accounting_titles(id),
    ADD COLUMN IF NOT EXISTS accounting_title_code VARCHAR(20),
    ADD COLUMN IF NOT EXISTS accounting_title_name VARCHAR(100);

WITH category_title(category, code) AS (
    VALUES
    ('rent_payment', '4603'),
    ('electricity_payment', '4605'),
    ('deposit_refund', '4602'),
    ('deposit_deduction', '4601'),
    ('journal_expense', '6681')
)
UPDATE accounting_entries ae
SET
    accounting_title_id = at.id,
    accounting_title_code = at.code,
    accounting_title_name = at.name
FROM category_title ct
JOIN accounting_titles at ON at.code = ct.code
WHERE ae.category = ct.category
  AND (
      ae.accounting_title_id IS NULL
      OR ae.accounting_title_code IS NULL
      OR ae.accounting_title_name IS NULL
  );

WITH category_title(category, code) AS (
    VALUES
    ('rent_payment', '4603'),
    ('electricity_payment', '4605'),
    ('deposit_refund', '4602'),
    ('deposit_deduction', '4601'),
    ('journal_expense', '6681')
)
UPDATE monthly_snapshot_entries mse
SET
    accounting_title_id = at.id,
    accounting_title_code = at.code,
    accounting_title_name = at.name
FROM category_title ct
JOIN accounting_titles at ON at.code = ct.code
WHERE mse.category = ct.category
  AND (
      mse.accounting_title_id IS NULL
      OR mse.accounting_title_code IS NULL
      OR mse.accounting_title_name IS NULL
  );

CREATE INDEX IF NOT EXISTS idx_accounting_entries_accounting_title_id
    ON accounting_entries (accounting_title_id);

CREATE INDEX IF NOT EXISTS idx_monthly_snapshot_entries_accounting_title_id
    ON monthly_snapshot_entries (accounting_title_id);

CREATE INDEX IF NOT EXISTS idx_legacy_accounting_title_mappings_title_id
    ON legacy_accounting_title_mappings (accounting_title_id);

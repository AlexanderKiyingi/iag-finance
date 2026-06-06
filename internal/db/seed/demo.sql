-- Demo UI rows (iag-finance.html). Idempotent per startup when SEED_ON_STARTUP=true.

DELETE FROM table_rows
WHERE table_id IN ('seed_coa', 'seed_bank_cash', 'seed_ap_inbox', 'seed_cherry_intake');

INSERT INTO table_rows (table_id, row_html) VALUES ('seed_coa', $c01$
<tr><td class="code"><b>1100</b> · Cash &amp; cash equivalents</td><td>Asset</td><td>Debit</td><td>Entity · bank required</td><td class="num">UGX 2,852M</td><td><span class="pill ok">ACTIVE</span></td></tr>
$c01$);

INSERT INTO table_rows (table_id, row_html) VALUES ('seed_coa', $c02$
<tr><td class="code"><b>1300</b> · Inventories</td><td>Asset</td><td>Debit</td><td>Entity · lot required</td><td class="num">UGX 1,920M</td><td><span class="pill ok">CONTROL</span></td></tr>
$c02$);

INSERT INTO table_rows (table_id, row_html) VALUES ('seed_coa', $c03$
<tr><td class="code"><b>1400</b> · AR Trade</td><td>Asset</td><td>Debit</td><td>Customer required</td><td class="num">UGX 2,228M</td><td><span class="pill ok">CONTROL</span></td></tr>
$c03$);

INSERT INTO table_rows (table_id, row_html) VALUES ('seed_coa', $c04$
<tr><td class="code"><b>2100</b> · AP Trade</td><td>Liability</td><td>Credit</td><td>Vendor required</td><td class="num">UGX 880M</td><td><span class="pill ok">CONTROL</span></td></tr>
$c04$);

INSERT INTO table_rows (table_id, row_html) VALUES ('seed_coa', $c05$
<tr><td class="code"><b>4000</b> · Export revenue</td><td>Revenue</td><td>Credit</td><td>Customer · lot required</td><td class="num">USD 3.84M</td><td><span class="pill ok">ACTIVE</span></td></tr>
$c05$);

INSERT INTO table_rows (table_id, row_html) VALUES ('seed_coa', $c06$
<tr><td class="code"><b>5310</b> · Utilities</td><td>Expense</td><td>Debit</td><td>Cost centre required</td><td class="num">UGX 42M</td><td><span class="pill ok">ACTIVE</span></td></tr>
$c06$);

INSERT INTO table_rows (table_id, row_html) VALUES ('seed_bank_cash', $b01$
<tr><td class="code"><b>1110</b> · MoMo Float</td><td>MTN MoMo</td><td>UGX</td><td class="num">678,000,000</td><td><span class="pill ok">DAILY</span></td><td>Farmer payouts</td></tr>
$b01$);

INSERT INTO table_rows (table_id, row_html) VALUES ('seed_bank_cash', $b02$
<tr><td class="code"><b>1120</b> · Operating</td><td>Stanbic UG</td><td>UGX</td><td class="num">1,820,000,000</td><td><span class="pill ok">98%</span></td><td>H2H bulk pay</td></tr>
$b02$);

INSERT INTO table_rows (table_id, row_html) VALUES ('seed_bank_cash', $b03$
<tr><td class="code"><b>1130</b> · USD Account</td><td>Stanbic UG</td><td>USD</td><td class="num">354,000</td><td><span class="pill ok">98%</span></td><td>Export receipts</td></tr>
$b03$);

INSERT INTO table_rows (table_id, row_html) VALUES ('seed_ap_inbox', $a01$
<tr style="cursor:pointer"><td class="code"><b>INV-AP-2026-04412</b></td><td>Mukwano Industries</td><td class="num">UGX 1.79M</td><td class="code">07 Jun</td><td><span class="pill ok"><span class="dot"></span>3-WAY OK</span></td></tr>
$a01$);

INSERT INTO table_rows (table_id, row_html) VALUES ('seed_ap_inbox', $a02$
<tr><td class="code">INV-AP-2026-04411</td><td>UEDCL</td><td class="num">UGX 4.20M</td><td class="code">12 May</td><td><span class="pill warn"><span class="dot"></span>NEEDS APPR</span></td></tr>
$a02$);

INSERT INTO table_rows (table_id, row_html) VALUES ('seed_ap_inbox', $a03$
<tr><td class="code">INV-AP-2026-04410</td><td>NWSC</td><td class="num">UGX 0.84M</td><td class="code">15 May</td><td><span class="pill ok"><span class="dot"></span>3-WAY OK</span></td></tr>
$a03$);

INSERT INTO table_rows (table_id, row_html) VALUES ('seed_cherry_intake', $i01$
<tr style="cursor:pointer"><td class="code">CI-09472</td><td>Tugume Bosco</td><td class="num">47.0</td><td class="num">166,850</td><td><span class="pill warn"><span class="dot"></span>PAY NOW</span></td></tr>
$i01$);

INSERT INTO table_rows (table_id, row_html) VALUES ('seed_cherry_intake', $i02$
<tr><td class="code">CI-09471</td><td>K. Asasira</td><td class="num">52.4</td><td class="num">170,300</td><td><span class="pill ok"><span class="dot"></span>PAID</span></td></tr>
$i02$);

INSERT INTO table_rows (table_id, row_html) VALUES ('seed_cherry_intake', $i03$
<tr><td class="code">CI-09470</td><td>M. Tumusiime</td><td class="num">38.2</td><td class="num">124,150</td><td><span class="pill ok"><span class="dot"></span>PAID</span></td></tr>
$i03$);

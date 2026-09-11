# PLAN.md — Aplikasi Rekonsiliasi SPBU (nama TBD)

Status: draft v17 (BUILD TARGET — resolusi review putaran 15; loop maks 25 putaran)
Bahasa UI: Seluruh copywriting wajib Bahasa Indonesia (ID). Dokumen teknis memakai ASD-STE100 (Simplified Technical English).

## 1. Tujuan

Aplikasi multi-vendor untuk pelaporan dan monitoring operasi SPBU per shift.
Fokus MVP: pencegahan fraud pada pencatatan losses/gains dan penjualan.
Latar belakang: kerugian IDR 200 juta karena supervisor mencatat 100L sebagai losses padahal kehilangan uang.

## 2. Ruang Lingkup MVP

Termasuk:
- 1 organisasi, N SPBU. Input manual. Rekonsiliasi per nozzle.
- DUA varian rekonsiliasi terpisah (liter + Rupiah), dua threshold.
- Laporan immutable + amendment + audit trail + policy versioning + printout.

Ditunda: rekonsiliasi kas, ack Operator, notifikasi eksternal, integrasi dispenser, koreksi suhu, multi-organisasi.

## 3. Peran, Izin, dan Pemisahan Tugas

| Peran | Cakupan | Kewenangan MVP |
|---|---|---|
| Operator | 1 SPBU | Akun saja; aksi ditunda |
| Supervisor | 1 SPBU | Mulai/selesai shift; input; printout; ajukan amendment |
| Admin SPBU | 1..N SPBU | Ack shift; approve amendment; generate_report; view_all_stations; manage_dispensers |
| Owner | Organisasi | Buat user; approve amendment; audit/reports/anomali; ubah kebijakan; fallback ack |
| Superadmin | Semua | Break-glass wajib ber-alasan; ter-audit |

SoD (server-enforced, per user_id, deny precedence, evaluator per-aksi):
1. Pembuat data tidak bisa meng-ack shift berisi datanya. 2. Requester ≠ approver amendment. 3. Satu amendment aktif per versi. 4. Multi-role boleh; SoD tetap. 5. `ack_shift` hanya efektif bagi Admin SPBU/Owner. 6. Owner tidak menginput operasional.
Break-glass: Owner/Superadmin boleh menembak SoD, wajib alasan non-kosong (divalidasi DB/API), ter-audit (`is_superadmin`/`is_break_glass`), masuk daftar anomali.

Permission flag: `ack_shift`, `approve_amendment`, `manage_users`, `generate_report`, `view_all_stations`, `manage_dispensers`.

## 4. Skema Data

- ack_decisions schema eksplisit (parent UNIQUE(org_id, station_id, ack_id)): id PK, org_id, station_id, shift_id, report_id, version_no; FK komposit (org_id, station_id, shift_id, report_id, version_no) → shift_reports parent UNIQUE(org_id, station_id, shift_id, report_id, version_no) — DEFINISI RESMI parent ini; ack_seq integer, decision: acked|rejected, actor, decided_at, rejection_reason, is_superadmin, is_break_glass, break_glass_reason.
- LOSS constraint canonical (SATU): loss_entries UNIQUE(org_id, station_id, shift_report_id, loss_logical_id) — tenant-complete; semua FK loss komposit tenant (parent key sesuai katalog §4).
- Draft children parent keys eksplisit: draft_readings UNIQUE(org_id, station_id, draft_row_id) (row_id PK ditambahkan); draft_sales, draft_losses, draft_evidence_staging sama (row PK + parent tenant key + FK komposit ke parent).
- policy_snapshot_items FK: (org_id, station_id, shift_id, set_id, item_id) FK → parent UNIQUE(org_id, station_id, shift_id, set_id, item_id) pada items.
- source-reading FK: parent UNIQUE(org_id, station_id, shift_id, report_id, reading_id) pada dispenser_readings (shift_id + report_id kolom eksplisit di dispenser_readings; report_id kolom); FK (org_id, station_id, source_shift_id, source_shift_report_id, source_reading_row_id) komposit exact — semua kolom eksplisit.
- loss_entries FK parent: UNIQUE(org_id, station_id, shift_id, report_id, row_id) — FK komposit exact (org_id, station_id, shift_id, shift_report_id, loss_row_id).
- ack_decisions: kolom id DIRENAME ack_id (konsistensi parent key: UNIQUE(org_id, station_id, ack_id)); semua referensi memakai (org_id, station_id, ack_id).
- idempotency: kolom lease_started_at + lease_expires_at (absolut, 10 menit dgn heartbeat tiap 60 detik memperpanjang; heartbeat claim-gated). Retry: key SAMA + payload SAMA = request_hash SAMA (bukan baru); payload berubah = 409; retry failed = INSERT ulang dgn idempotency_key BARU.
- RLS + authorization (MODEL FINAL): role matrix — app role: hanya EXECUTE pada SECURITY DEFINER prosedur + SELECT pada SECURITY DEFINER views (BUKAN security_invoker). SECURITY DEFINER berjalan sebagai owner role `app_data_owner` (NOBYPASSRLS, owner tabel, FORCE ROW LEVEL SECURITY aktif di SEMUA tabel; row_security=off DIHAPUS — owner dengan FORCE RLS tetap ter-RIS kecuali policy policy_for_owner=True yang membaca role dari set_config yang di-set prosedur setelah verifikasi token). Prosedur selalu men-cek tenant eksplisit sebelum query ( defense-in-depth dua lapis: RLS policy berbasis set_config + cek manual). Tidak ada EXECUTE pada view.
- CURRENT-POINTER parent key: shift_reports UNIQUE(org_id, station_id, shift_id, report_id) DIDEKLARASIKAN RESMI (lihat definisi shift_reports); FK pointer (org_id, station_id, shift_id, current_report_id) mengikat exact.

### 4.1 Master data
- organizations, stations (timezone, versi: timezone dapat berubah; shift menyimpan snapshot timezone)
- nozzles (meter_max numeric(10,1) = nilai tampilan maksimum; modulus efektif = meter_max + 0.1 karena counter berhenti di 99999.9 lalu ke 0.0; meter decimal(10,1))
- nozzle_tank_map (nozzle, tank, tstretch tstzrange; EXCLUDE USING gist non-overlap)
- dispenser_prices (nozzle, harga numeric(14,0), tstretch tstzrange, created_by; exclusion non-overlap)

### 4.2 Operasional
- shifts (org_id, station_id, supervisor_id, opened_at, closed_at, timezone_snapshot, business_date, status enum: open|submitting|failed|abandoned|awaiting_confirmation|needs_correction|locked; ...; partial UNIQUE(org_id, station_id, original_event_date, shift_ke) WHERE backfilled AND original_event_date IS NOT NULL)
- AMENDMENT ELIGIBILITY (SATU aturan final): amendment approval HANYA bila base_report = shifts.current_report_id SAAT approval (guard di dalam shift-row lock, re-cek stale hash). Base boleh SUBMITTED atau LOCKED. LOCKED base → versi baru + supersede ack lama. Stale (bukan current) → 409, ajukan ulang.
- shift_drafts (org_id, station_id, shift_id, owned_by, claim_token uuid, claim_expires_at timestamptz, status: editing|submitting|submitted|failed|recovering, updated_by, updated_at, revision integer; UNIQUE(shift_id) — satu draft per shift SEUMUR HIDUP; claim = UPDATE WHERE status='editing' AND (claim_expires_at IS NULL OR claim_expires_at < now()) SET claim_token+expiry (lease 5 menit); recovery job: submitting > 10 menit → recovering → editing/failed; failed → retry = status editing dengan revision++ ; setiap mutasi child menaikkan revision) + TABEL DRAF ANAK (semua membawa org_id+station_id, FK komposit ke shift_drafts):
  - draft_readings (draft_id, nozzle_id, meter_start, meter_end, created_by; UNIQUE(draft_id, nozzle_id))
  - draft_sales (draft_id, dispenser_id, cash_amount, cashless_amount, created_by; UNIQUE(draft_id, dispenser_id))
  - draft_losses (draft_id, loss_id immutable PK (= loss_logical yang dipromosikan), direction, reason_code, liters, cash_amount, note, created_by)
  - draft_evidence_staging (draft_id, loss_id FK draft_losses, status: uploaded|verified|finalized, object_key, content_hash, size, mime, uploaded_by)
- submit_idempotency (idem_id PK UUID, org_id, station_id, shift_id, idempotency_key, request_hash, status: in_progress|succeeded|failed, claim_token, attempt_count, lease_started_at NOT NULL, lease_expires_at NOT NULL (absolut; SERAGAM: takeover & stale = lease_expires_at < now() — SATU timeout 10 menit; heartbeat tiap 60 detik memperpanjang lease_expires_at, claim-gated), resulting_report_id nullable, error_detail nullable, created_at, updated_at; UNIQUE(org_id, station_id, shift_id, idempotency_key)). PROTOCOL (SATU definisi): (1) INSERT ... ON CONFLICT DO NOTHING; menang = pemilik. (2) kalah → baca: succeeded → kembalikan report lama (re-otorisasi); in_progress + belum expired → 409; in_progress + expired → TAKEOVER (conditional UPDATE ... RETURNING; 0 rows = 409). (3) HASH: canonical hash = sha256(JSON kanonik payload TANPA idempotency_key); payload SAMA dgn key SAMA = hash SAMA; payload berubah (key sama) = 409 SELALU; retry failed = INSERT ulang dgn key BARU. (4) Finalisasi claim-gated; 0 rows → ROLLBACK seluruh tx report. Report+set succeeded = SATU transaksi; set failed = transaksi terpisah.
- shift_reports (id = report_id UUID global PK; org_id, station_id, shift_id; UNIQUE(org_id, station_id, shift_id, version_no); parent key utk FK = UNIQUE(org_id, station_id, report_id); supersedes_report_id → FK (org_id, station_id, shift_id, supersedes_report_id) + trigger cek same-shift; TIDAK ADA kolom is_current — sumber kebenaran tunggal = shifts.current_report_id, FK komposit (org_id, station_id, shift_id, current_report_id) — report dari shift lain tidak mungkin; status: submitted|locked; submitted_by, submitted_at; policy_snapshot_set_id → policy_snapshot_sets (bukan policy_snapshots); versi fisik loss/evidence per version_no)
- policy_snapshots: snapshot_set model — policy_snapshot_sets (set_id PK, org_id, station_id, shift_id, created_at; parent UNIQUE(org_id, station_id, set_id)) + policy_snapshot_items (item_id PK, set_id FK komposit, policy_kind: threshold|evidence, policy_id, rev_id, scope, canonical payload jsonb (payload utuh bundle per kind — satu item per kind), hash sha256; UNIQUE(set_id, policy_kind); UNIQUE(rev_id) dalam set). shift_reports.policy_snapshot_set_id → FK komposit (org_id, station_id, shift_id, set_id). COMPLETENESS: deferred trigger mengecek set punya ≥1 item threshold + 1 item evidence sebelum report bisa disubmit. Tombstone: resolusi henti pada revisi disabled → aturan tidak berlaku.
- dispenser_readings (reading_id PK UUID, org_id, station_id, shift_report_id, nozzle_id, meter_start, meter_end, price_used, expected_sale_rupiah, created_by, observed boolean default true, is_carried_forward boolean default false, source_shift_report_id nullable FK, source_reading_row_id nullable FK (wajib bila is_carried_forward; CHECK: is_carried_forward=true ⇔ kedua source kolom NOT NULL); parent key UNIQUE(org_id, station_id, reading_id); UNIQUE(shift_report_id, nozzle_id))
- sales_declared (sales_id PK UUID, org_id, station_id, shift_report_id, dispenser_id, cash_amount REQUIRED, cashless_amount REQUIRED default 0, created_by; parent key UNIQUE(org_id, station_id, sales_id); UNIQUE(shift_report_id, dispenser_id)) — amendment men-target sales_id
- submit_idempotency (idem_id PK UUID, ...; protocol finalisasi: jika final UPDATE claim-gated menghasilkan 0 rows → ROLLBACK seluruh transaksi report (report tidak dibuat) → baca ulang status; heartbeat: worker update updated_at tiap 60 detik; submit dibatasi maks 10 menit (lease); retry: request_hash SAMA untuk payload sama (bukan baru), idempotency_key baru hanya utk payload berbeda — hash mismatch selalu 409)
- loss_identity: tabel pemisah loss_logical (org_id, station_id, loss_id PK global; dibuat sekali per klaim) + loss_entries versi (org_id, station_id, shift_report_id, loss_logical_id FK, row_id PK; UNIQUE(shift_report_id, loss_logical_id)); amendment men-target loss_logical_id; evidence_event scoped (org_id, station_id, shift_report_id, loss_logical_id, loss_row_id) — snapshot evidence per report version immutable; event per loss punya event_seq integer + legal transition (uploaded→verified→finalized) + per-loss lock saat insert event (prosedur).
- loss_exception: (id PK, org_id, station_id, shift_report_id, loss_logical_id, reason NOT NULL, actor NOT NULL, at NOT NULL; FK komposit ke loss_entries versi (parent UNIQUE(org_id, station_id, shift_report_id, loss_logical_id)); UNIQUE(org_id, station_id, shift_report_id, loss_logical_id) — satu exception per report-version/loss, INSERT-only; append-only DB protection).
- amendment target scope: setiap amendment_item wajib target benar-benar ADA di snapshot base report (prosedur cek: target_logical_id ada di child rows report versi base — bukan cuma same-shift).
- meter_reset_events (org_id, station_id, nozzle FK komposit, old_value (harus = meter_end laporan locked terakhir nozzle itu — di-cek prosedur), new_value, effective_shift_id FK komposit, reason, actor, approver ≠ actor, approved_at, status pending|approved; UNIQUE(org_id, station_id, nozzle, effective_shift_id) WHERE status='approved'; URUTAN: reset hanya dibuat jika nozzle punya ≥1 laporan locked ATAU via baseline awal eksplisit saat provisioning; reset approval ikut station lock yang sama dgn open/submit/backfill — diserialisasi per station+nozzle)
- amendments (org_id, station_id, base_report_id FK, reason, status pending|approved|rejected|superseded, requester, approver, decided_at, rejection_reason, applied_report_id FK same-shift (org_id, station_id, shift_id sama dengan base), stale_check_hash, is_break_glass + break_glass_reason nullable; partial UNIQUE(base_report_id) WHERE status='pending'; approval = transaksi atomik: lock shift+current report → verifikasi stale_check_hash → clone penuh report snapshot (semua child rows) → terapkan hanya allowlist → recompute anomali/evidence → set applied_report_id). Items: amendment_items (amendment_id, org_id, station_id, shift_id, target_kind enum, target_logical_id, field, old_value, new_value).
- ack_supersessions (id PK, org_id, station_id, shift_id, superseded_ack_id FK komposit → ack_decisions parent UNIQUE(org_id, station_id, ack_id), replacement_report_id FK komposit (org_id, station_id, shift_id, report_id) (wajib = report current BERIKUTNYA, version = old+1), reason, created_at; UNIQUE(superseded_ack_id); INSERT-only). Invariant atomik (SATU transaksi, shift-row lock): old report = current → buat report baru (version+1) → INSERT supersession (replacement=new report) → pindah current_report_id. Terminologi: event_type vocabulary = fired|cleared (konsisten di semua tabel alert). JWT key registry: tabel jwt_keys (kid PK, secret_ref, status active|previous|retired, activated_at, retired_at; rotasi dua fase: new=active, old=previous, retire old saat semua token dgn exp ≤ now aktif-time; EXECUTE inventory: setiap prosedur wajib terdaftar di tabel procedure_registry (name, allowed_roles) — CI men-cek prosedur tanpa klasifikasi = gagal).
- alert_events: period_bucket timestamptz GENERATED (kolom fisik, IMMUTABLE): date_bin(interval '1 hour', period_start, timestamptz '1970-01-01 00:00:00+00') — bucket per jam UTC; SATU constraint fired (subject bucket — rule period_start LAMA dihapus); alert_rules dedup line diganti merujuk constraint sama.
- audit FAILED-request durability: denied request ditulis ke tabel audit_denied (append-only, transaksi TERPISAH via koneksi pool berbeda — bukan outbox yang ikut rollback). audit_outbox hanya utk event yang sukses.
- amendment_items: kolom: (item_id PK, amendment_id FK, org_id, station_id, shift_id, target_kind enum: sales_declared|loss_entry|delivery|dip_reading, target_logical_id, field, old_value, new_value) — target_logical_id = sales_id / loss_id(logical) / delivery_id / dip_id (semua PK UUID yang ada); FK/prosedur cek scope (org+station+shift sama dgn base report) di-enforce per kind.
- deliveries (id PK, org_id, station_id, shift_id, do_number, tank, liters, created_by; FK komposit ke shifts) dan dip_readings (id PK, org_id, station_id, shift_id, tank, dip_liters, created_by; FK komposit ke shifts) — snapshot tables per report version: delivery_snapshots & dip_snapshots (shift_report_id FK, delivery_id/dip_id FK sumber, same-shift FK komposit). Carried-forward reading source FK: (org_id, station_id, shift_id, source_shift_report_id, source_reading_row_id) FK komposit + prosedur cek: source = predecessor locked report terakhir utk nozzle sama.
- numeric types: meter numeric(10,1) dengan CHECK 0≤nilai≤meter_max; volume numeric(8,2) CHECK ≥0; uang numeric(14,0) — nilai maksimum 99.999.999.999.999 (≈ 99,9 triliun rupiah, cukup untuk MVP); harga master numeric(14,0) (bukan integer — konsisten dgn tipe uang); semua perhitungan numeric (bukan float); OVERFLOW: dicek SEBELUM rounding/casting di TIAP tahap (perkalian per reading, SUM agregasi, varian); hasil > max → RAISE → tx rollback → 22003 → 422; volume SUM juga dibatasi numeric(12,2); varian boleh negatif (dgn batas |varian| ≤ max); boundary tests: half-up .5, meter_max, overflow pengali, aggregate overflow, JSON/TS konversi (string di JSON, hindari float).
- LOSS/amendment constraint canonical: SATU definisi per constraint (lihat §4.2); amendment FK procedural checks didokumentasikan eksplisit (target-in-snapshot, per-kind scope — polymorphic, tidak bisa jadi FK statis).
- GATE BARU (CI/pilot wajib): canonical hashing (same-key/same-payload), heartbeat fencing, auto-abandoned transition, policy completeness+immutability, UTC alert bucket (cross-midnight), baseline cross-nozzle isolation, overflow 22003→422, tenant-FK catalog enforcement (pg_constraint).
- dispenser↔nozzle: 1 dispenser boleh >1 nozzle; sales_declared per dispenser = AGREGAT klaim (bukan derived dari nozzle); varian Rupiah = expected (Σ nozzle) − declared (Σ dispenser); mapping dispenser↔nozzle disnapshot di shift_price_map_snapshot (utk printout & audit, bukan perhitungan varian).
- alert_rules (id PK, org_id, station_id, rule_type, alert_key UNIQUE(org_id, station_id, alert_key), threshold, channel, enabled, created_by). occurrence lock = FOR UPDATE pada alert_rules row. Validasi fired/cleared di PROSEDUR. Dedup & re-alert = lihat alert_events constraint (subject bucket) — tidak ada rule period_start terpisah.
- baseline: model REVISI + POINTER. nozzle_baseline_revisions (baseline_rev_id PK, org_id, station_id, nozzle_id, initial_meter_value, meter_max numeric(10,1) NOT NULL, created_by, created_at; parent UNIQUE(org_id, station_id, baseline_rev_id) + UNIQUE(org_id, station_id, nozzle_id, baseline_rev_id); INSERT-only) + nozzle_baseline_current (org_id, station_id, nozzle_id, current_baseline_rev_id; FK komposit (org_id, station_id, nozzle_id, current_baseline_rev_id) → revision parent key 4-kolom — pointer TIDAK BISA menunjuk revision nozzle lain; parent UNIQUE(org_id, station_id, nozzle_id); UPDATE hanya via prosedur owner-approval: buat revision baru → pindah pointer, SATU transaksi). CHECK initial_meter_value ∈ [0, meter_max snapshot]. Chaining generator: meter_end locked report terakhir > reset approved > baseline current. Backfill invariants: backfill_status='approved' ⇔ backfill_approver & backfill_approved_at & backfill_reason NOT NULL; carried-forward: is_carried_forward=true ⇔ source kolom NOT NULL (CHECK).
- LOCK ORDER GLOBAL (total order, SEMUA resource diranked, TANPA "other"): 1) stations row → 2) shifts row → 3) shift_drafts → 4) submit_idempotency row → 5) shift_reports current → 6) policy_snapshot sets → items → 7) alert_rules row → 8) audit org chain lock row (tabel audit_chain_locks: org_id PK) → 9) resource lain — SETIAP tabel diberi rank eksplisit di DDL manifest (kolom lock_rank); multi-row ascending; deadlock tests.
- Urutan submit: konsisten dgn rank: draft (3) → idempotency (4) → report (5) — prosedur submit ikut urutan ini.
- ACK: ack_head table (report_id PK version_no, active_ack_id FK) — pointer aktif; alokasi ack_seq + update pointer dalam SATU transaksi dgn report-row lock (rank 5); supersession INSERT atomik dgn pointer move. Ini mencegah dua ack aktif concurrent.
- Ack cardinality: tepat SATU ack aktif per (report version) — aktif = ack_seq max & tanpa supersession; ditegakkan prosedur + deferred trigger + concurrency test. Rejected juga satu aktif (decision menyimpan rejected sebagai state aktif sampai digantikan).
- JWT sessions: sessions (jti PK, kid FK, issued_at, expires_at, revoked_at nullable; CHECK: expires_at − issued_at ≤ 15 menit; NumericDate strict); retire key previous saat expires_at max dari semua token yg diterbitkan dgn kid itu sudah lewat (kolom max_token_expiry di jwt_keys).
- Reset chronology: reset selection = approved reset terbaru dengan effective_shift_id ≤ target shift (urutan by shift chronology/business_date); future-effective reset (effective_shift belum terjadi) dikecualikan; seleksi under station+nozzle lock. Baseline/reset FK tenant-bound (org,station,nozzle) → nozzles.
- baseline: revisions juga FK komposit (org_id, station_id, nozzle_id) → nozzles (tenant-bound); pointer cross-nozzle test. JCS: payload schema didefinisi (field list tetap, defaults dinormalisasi server, decimal sebagai string, array terurut, unknown fields ditolak, hash_version field di payload); semua aritmetika intermediate pakai numeric penuh sebelum dibatasi ke kolom.
- audit_log hash chain: kolom prev_hash, org_sequence bigint (per-org; di-alokasikan DI DALAM append lock per-org — resource lock = row di audit_chain_locks (org_id PK)), row_hash = sha256(RFC8785 canonical bytes = [hash_version, org_id, org_sequence, event_id, event_type, payload, created_at, prev_hash]); genesis: org_sequence=1 dgn prev_hash = 32 byte nol; UNIQUE(org_id, org_sequence); verifier: baca berurutan by org_sequence → recompute chain → 0 gap; tamper test. audit_denied TIDAK bagian chain. Business mutation + audit_outbox record = SATU transaksi (atomic commit).
- Canonical payload schema per hash domain (VERSIONED): setiap domain (idempotency, stale_check, snapshot, audit) punya JSON Schema versioned (hash_version field); field list tetap; defaults server-normalized; decimal = string; bigint > 2^53 = string; null diperbolehkan hanya bila schema bilang; arrays terurut dgn sort key eksplisit; unknown fields ditolak. Fixtures di repo utk tiap domain.
- active-shift lifecycle (SATU definisi otoritatif — definisi ganda dihapus): status enum = open|submitting|failed|abandoned|awaiting_confirmation|needs_correction|locked. Transisi: open → submitting; submitting → awaiting_confirmation | failed; failed → open (retry, via REOPEN jika belum ada report) | abandoned (auto > 24 jam, prosedur scheduler — BUKAN needs_correction; abandoned TIDAK mengunci SPBU, tidak masuk active-shift unique); awaiting_confirmation → locked (acked) | needs_correction (rejected); needs_correction → awaiting_confirmation (via amendment approved — HANYA jika ada report; guard). Active-shift partial unique: status IN (open, submitting, failed, awaiting_confirmation). needs_correction selalu berarti ada report (guard prosedur).
- gate matrix tambahan (concurrency/integrity tests): stale idempotency takeover race (2 worker, 1 pemenang); ack supersession (lama non-aktif setelah amendment); pointer FK isolation (report dari shift lain ditolak); policy tombstone (aturan disabled → aturan tak berlaku); audit outage (fail-closed, request ditolak); reset baseline ordering (reset tanpa locked report & tanpa baseline = ditolak); alert source uniqueness (2 clear utk 1 fired = ditolak).
- shift_transitions (org_id, station_id, shift_id, from_status, to_status, actor, at, reason) — riwayat status untuk alert starvation & audit
- Actor identity: DIPUTUS (TUNGGAL) — signed session JWT. Klaim: iss (tetap), sub (user_id), jti (unik, untuk revoke), iat/exp (exp 15 menit, clock skew toleransi 60 detik), aud (tetap: "spbu-recon"), alg PINNED HS256 (menolak alg lain/no alg). VERIFIKASI URUTAN LENGKAP: (1) alg==HS256, (2) kid resolve di jwt_keys (status active|previous; iat ≤ exp; exp dgn skew; iat tidak di masa depan), (3) signature, (4) iss sama, (5) aud sama, (6) exp belum lewat (skew), (7) iat valid (skew), (8) jti: ada di sessions DAN belum revoked. Key rotation: prev dirilis setelah grace = max exp token yg diterbitkan dgn prev. Session: satu jti per login; logout = revoked_at. Identitas hanya dari token terverifikasi; klien tidak kirim actor_id. Raw JWT tidak disimpan di audit (hanya sub+jti).
- audit_log (skema v4 + outcome_error; APPEND-ONLY: tabel dimiliki role khusus; role aplikasi TIDAK punya INSERT/UPDATE/DELETE/SELECT langsung — insert hanya via SECURITY DEFINER fn (search_path fixed); REVOKE semua DML+SELECT dari semua role aplikasi; failed-request ditulis ke audit_denied via transaksi/koneksi TERPISAH (durable, tidak ikut rollback); FAIL-CLOSED: denial response hanya dikirim SETELAH audit_denied commit sukses — jika path audit down, request GAGAL (fail closed). audit_denied payload minimum: request_id, actor/token identity, tenant, action, target, reason, server_timestamp, outcome. Outbox: audit_outbox (event_id UUID PK, event_type, payload jsonb, created_at; INSERT-only) + outbox_relay_state (mutable oleh relay: relay_status, attempt_count).

### 4.3 Kebijakan
- threshold_policies / evidence_policies: model REVISI APPEND-ONLY. Setiap revisi = baris baru dengan valid_from (timestamptz) + supersedes_rev_id (menunjuk revisi LAMA yang digantikan — arah ke belakang, ditulis saat baris BARU dibuat; baris lama TIDAK pernah di-update). RESOLUSI as-of: pilih revisi TERBARU dengan valid_from ≤ resolution_time (TIDAK bergantung pada status superseded — revisi masa depan tidak menghapus keberlakuan revisi saat ini pada masa kini). Scope precedence: SPBU > org; UNIQUE(scope, valid_from); satu successor per revisi; tombstone = revisi baru dengan flag disabled. Baris terpilih disnapshot (policy_id + rev_id + hash) di laporan.
- Resolusi: threshold saat submit; evidence saat submit; harga saat BUKA shift.

## 5. Aturan Detail

### 5.1 Meter
- meter decimal(10,1); meter_max = nilai tampilan maksimum (mis. 99999.9); MODULUS = meter_max + 0.1 (siklus penuh 99999.9 → 0.0, sehingga delta benar).
- Rollover: jika meter_end < meter_start, delta = (meter_end − meter_start) + MODULUS; valid HANYA jika 0 ≤ meter_start, meter_end ≤ meter_max AND (MODULUS − meter_start) + meter_end ≤ rollover_threshold (default 20% MODULUS, configurable) — membatasi delta maksimum rollover, bukan hanya posisi start. Maks 1 rollover per reading; multi-rollover ditolak.
- Reset: meter_reset_events menyimpan old/new value + effective_shift_id (shift pertama yang memakai new value); chaining: meter_start shift berikutnya = meter_end laporan locked sebelumnya ATAU new_value reset yang efektif.
- Delta ≥ 0 selalu (setelah rumus rollover). UNIQUE(shift_report_id, nozzle_id). Chaining server-enforced.
- Amendment TIDAK BOLEH menyentuh field meter pada laporan status APA PUN (submitted maupun locked) — sesuai keputusan produk; allowlist amendment mengecualikan meter path. Koreksi meter = shift baru atau reset event.

### 5.2 Harga & mapping
- Server-resolve; resolusi saat buka shift; FOR UPDATE pada stations + exclusion constraints; jadwal harga ke depan boleh.
- Snapshot harga + mapping + meter_max/modulus + ID master data ke shift saat BUKA shift (shift_price_map_snapshot jsonb; skema jsonb NOT NULL tervalidasi di prosedur open + hash sha256 disimpan; kolom IMMUTABLE via trigger). SEMUA mutasi harga/mapping MEMWAJIBKAN lock yang sama (SELECT FOR UPDATE stations) — satu aturan lock untuk semua penulis.

### 5.3 Lifecycle
- Shift: open → awaiting_confirmation → (acked → locked | rejected → needs_correction). needs_correction → amendment approved → versi baru → awaiting_confirmation. Backfill = FLAG (backfilled=true), bukan status; mengikuti lifecycle sama (open→awaiting→locked); opened_at = waktu input nyata, original_event_date tersimpan terpisah dan dipakai sebagai business_date; idempotency key = (org_id, station_id, original_event_date, shift_ke) partial unique WHERE backfilled.
- needs_correction tidak memblokir shift baru; partial unique: maks 1 shift open/awaiting_confirmation per SPBU.
- Report invariant: sumber kebenaran tunggal = shifts.current_report_id (TIDAK ada is_current boolean); setiap perpindahan pointer = UPDATE shifts.current_report_id dalam prosedur dengan shift-row lock; ack selalu pada current report. Report hanya bisa diamend saat status submitted (belum locked); setelah locked hanya via amendment → versi baru (tanpa meter path).
- Approval amendment transaksi atomik (lock + stale-check + create + link). Amendment rejected/superseded tidak mengubah data.
- PROTEKSI DML (matrice grants eksplisit): REVOKE ALL (termasuk SELECT) dari PUBLIC & role aplikasi pada SEMUA tabel: shifts, shift_drafts (+children), shift_reports, dispenser_readings, sales_declared, loss_logical/loss_entries/loss_exception, evidence_event, amendments, amendment_items, ack_decisions, ack_supersessions, shift_transitions, alert_rules, alert_events, threshold/evidence policies, policy_snapshot_sets/items, submit_idempotency, meter_reset_events, nozzle_baseline, dispenser_prices, nozzle_tank_map, deliveries, dip_readings, delivery_snapshots, dip_snapshots, sessions, audit_log, audit_denied, audit_outbox, outbox_relay_state, organizations, stations, nozzles, dispensers, tanks, users, user_station_roles. SETIAP tabel baru wajib masuk matriks (CI cek katalog: tabel tanpa klasifikasi privilege = gagal build; termasuk cek views security_invoker + EXECUTE prosedur terbatas). View = security_invoker (RLS tetap aktif untuk pemanggil). Relay role: UPDATE outbox_relay_state ONLY.
- current-pointer invariant: shifts.current_report_id NULL sebelum ada report; non-NULL & valid (submitted/locked, same tenant+shift) saat ada report; pointer FK komposit (org_id, station_id, shift_id, current_report_id) → parent key UNIQUE(org_id, station_id, shift_id, report_id) pada shift_reports. Deferred constraint trigger men-cek invariant di akhir transaksi.
- amendment state machine (AUTHORITATIVE, satu definisi — definisi duplikat dihapus): pending → approved (→ report baru + applied_report_id) | rejected (dgn reason) | superseded (amendment baru untuk versi sama). approved tidak mengubah report lama; setiap state change ter-audit. ACK state machine (AUTHORITATIF): report tanpa ack → acked (→locked) atau rejected (→needs_correction); ack aktif = tanpa supersession row; amendment approved → INSERT supersession → report baru awaiting_confirmation.
- loss_entries versi: fields lengkap (row_id PK, org_id, station_id, shift_report_id FK komposit (org_id, station_id, shift_id, report_id), version_no, loss_logical_id FK, nozzle_id nullable, direction, reason_code, liters, cash_amount, note, created_by; UNIQUE(shift_report_id, loss_logical_id)). loss_exception scoped PER REPORT VERSION: (org_id, station_id, shift_report_id, loss_logical_id). evidence_event: PK evidence_id; kolom (org_id, station_id, shift_report_id, loss_logical_id, loss_row_id FK, event_seq, event_type, object_key, content_hash, size, mime, actor, at); UNIQUE(loss_row_id, event_seq); legal transitions (uploaded→verified→finalized) di prosedur; snapshot per report version immutable; hanya append-procedure insert.
- rollover config (rollover_threshold) ikut shift snapshot saat open.
- Policy snapshot integrity: snapshot items IMMUTABLE setelah ada report menunjuk set (trigger blok UPDATE/DELETE pada items bila set dirujuk report); completeness deferred trigger dievaluasi saat submit (same shift, final status).
- dispenser_nozzle_map master (AUTHORITATIVE): (id PK, org_id, station_id, dispenser_id, nozzle_id, tstretch tstzrange; EXCLUDE non-overlap per dispenser & per nozzle — satu nozzle hanya satu dispenser aktif; FK komposit). Disnapshot di shift_price_map_snapshot saat open.
- policy_snapshot_set FK exact: parent UNIQUE(org_id, station_id, shift_id, set_id) pada policy_snapshot_sets — FK (org_id, station_id, shift_id, set_id) mengikat exact.
- ack FK enforceable: FK komposit (org_id, station_id, shift_id, report_id, version_no) → shift_reports (parent UNIQUE(org_id, station_id, shift_id, report_id, version_no)); LOCK ORDER BAKU (satu urutan utk ack/amendment/pointer): lock shifts row → current shift_reports → target lain.
- LIFECYCLE SATU (sisa definisi lama §5.3 DIHAPUS — authoritative = blok ini): status enum shifts: open|submitting|failed|abandoned|awaiting_confirmation|needs_correction|locked. TRANSISI: open → submitting; submitting → awaiting_confirmation | failed; failed → open (retry/REOPEN) | abandoned (auto >24 jam scheduler); awaiting_confirmation → locked (acked) | needs_correction (rejected); needs_correction → awaiting_confirmation (amendment approved; juga dari LOCKED base via amendment — transisi eksplisit locked → awaiting_confirmation). Active-shift unique: open|submitting|failed|awaiting_confirmation.
- draft lease fencing: setiap mutasi child DAN submit wajib memverifikasi (claim_token match, claim_expires_at > now(), revision sama) dalam satu UPDATE ... WHERE — expired client ditolak. Recovery: submitting > 10 menit → recovering; CEK dulu submit_idempotency/report existence — jika report sudah ada → submitted (bukan editing); jika tidak → editing/failed. Recovery attempts dicatat (recovery_count).
- price/mapping: price_used & expected_sale_rupiah = SERVER-COMPUTED dari shift_price_map_snapshot (tidak bisa di-input klien); validasi membership nozzle/dispenser terhadap snapshot saat submit; semua insert/update/delete harga/mapping memakai station lock. Rupiah rounding: expected_sale_rupiah = ROUND(meter_delta × harga, 0), pembulatan HALF-UP per reading di prosedur submit (satu tempat, deterministik); agregasi = SUM nilai yang sudah dibulatkan; loss/gain cash_amount = integer rupiah. Rupiah arithmetic WAJIB numeric (bukan float); uji: nilai setengah (.5), overflow meter.

### 5.4 Rekonsiliasi dual-unit
- Grain: per nozzle; agregasi shift.
- Volume: metered_volume_shift = Σ nozzle delta_liter. Dua aturan TERPISAH: anomali_loss = Σ loss_liter > threshold loss_liter; anomali_gain = Σ gain_liter > threshold gain_liter (net loss−gain TIDAK dipakai agar gain besar tidak menutupi loss; policy net_liter opsional). loss_rupiah/gain_rupiah policy hanya dipakai utk klaim cash_amount pada loss_entries (sederhana: anomali jika Σ cash_amount klaim loss > threshold loss_rupiah). Klaim sales TIDAK dipakai dalam varian volume.
- Uang: varian_rupiah = Σ nozzle expected_sale_rupiah − Σ (cash + cashless) declared; anomali jika |varian_rupiah| > threshold variance_rupiah (default 0). cash_amount REQUIRED; cashless REQUIRED default 0.
- loss_rupiah rule: Σ cash_amount pada loss_entries (direction=loss) > threshold loss_rupiah. gain_rupiah rule: Σ cash_amount pada gain_entries (direction=gain) > threshold gain_rupiah. cash_amount nullable: baris tanpa cash_amount tidak ikut rule Rupiah (rule liter tetap berlaku) — BUKAN post-MVP.
- CHECK: liters ≥ 0; cash/cashless ≥ 0; UNIQUE(dispenser_id, shift_report_id) di sales_declared.
- loss_entries: liters wajib utk rule liter; cash_amount opsional (dipakai rule loss_rupiah/gain_rupiah; konversi penuh rekonsiliasi kas = post-MVP).
- DO/dip = informatif di v1 (tidak masuk rumus varian).

### 5.5 Evidence
- Kardinalitas: jenis diterima + jumlah minimal per loss didefinisi di evidence_policies (disnapshot).
- Mode `wajib`: loss_exception DILARANG — ditegakkan di prosedur submit + trigger (bukan row CHECK).

### 5.6b Amendment & Backfill enforcement
- Amendment: allowed-paths di-enforce oleh fungsi DB (bukan konvensi jsonb): hanya sales_declared (cash/cashless), loss_entries (liters/note/cash_amount), referensi DO/dip. Meter path ditolak. Requester ≠ pembuat data yang diubah (kecuali break-glass ber-alasan). Revalidasi aturan evidence saat amendment mengubah loss (pakai policy snapshot report): amendment approval meng-clone + relink evidence ke loss rows baru. stale_check_hash = sha256(payload kanonik report base + version_no); diverifikasi di transaksi approval.
- Backfill: original_event_date + approval Owner (approver, backfill_approved_at, backfill_reason tersimpan di shifts); idempotency = partial UNIQUE(org_id, station_id, original_event_date, shift_ke) WHERE backfilled (1 SPBU boleh >1 shift per event date: pagi/sore). Out-of-order backfill DILARANG bila ada laporan locked berikutnya yang nozzle-nya ter-chain (cek juga draft aktif); hanya klaim non-meter (loss/sales) boleh di-backfill, meter diisi dari snapshot chaining (dispenser_readings.observed=false, is_carried_forward=true). Approval backfill memakai station lock yang sama.
- Objek evidence content-addressed: object storage write-once (upload menolak hash yang sudah ada dengan isi beda); DB INSERT-only.
- Mode `opsional` tanpa evidence: wajib loss_exception; direview saat ack (tidak butuh approval terpisah).

### 5.6 Isolasi
- RLS + authorization layer; FK komposit tenant-konsisten (lihat §4).
- Superadmin/break-glass: satu-satunya bypass; wajib alasan; anomaly view = union(break-glass actions, superadmin actions, loss_exceptions, varian atas threshold).
- Negative-test matrix: setiap permukaan (termasuk export/jobs).

### 5.7 Audit & alert
- audit_log: role pemilik terpisah; insert via SECURITY DEFINER (search_path FIXED); REVOKE semua DML+SELECT dari role aplikasi; failed-request → audit_denied via transaksi terpisah yang dilakukan oleh AUDIT-WRITER prosedur (SECURITY DEFINER, dipanggil aplikasi SEBELUM mengirim response denial — fail-closed; timeout 3 detik, jika gagal request ditolak tanpa response). audit_denied idempoten via request_id UNIQUE; payload: request_id, sub, jti, tenant, action, target, reason, server_timestamp, outcome (termasuk failed submit, bukan hanya authorization denial).
- Enforcement evidence: kardinalitas diperiksa di PROSEDUR submit/amendment (bukan row CHECK) memakai policy snapshot milik report; loss_exception immutabel (INSERT-only).
- Alert starvation WAJIB; alert varian opt-in.

## 6. Layar

1. Login; 2. Dashboard Supervisor; 3. Form Input Shift (per nozzle, harga otomatis); 4. Input DO + Dip; 5. Amendment queue; 6. Ack queue (+ break-glass); 7. Daftar Anomali (termasuk break-glass/exception); 8. Laporan + printout; 9. Manajemen User + Dispenser/Nozzle/Harga/Reset; 10. Pengaturan kebijakan (versi); 11. Audit Trail (filter break-glass/superadmin).

## 7. Acceptance Criteria (MVP)

1. business_date benar: lintas tengah malam, submit terlambat, backfill; timezone disnapshot per shift; uji DST/timezone change.
2. Lock hanya setelah ack pembuat-data-berbeda; break-glass wajib alasan + ter-audit + muncul di anomali.
3. Dua threshold (liter & Rupiah) berjalan; varian volume & varian Rupiah = anomali berbeda; negative Rupiah variance terekam.
4. Amendment: allowlist field (tanpa meter), requester ≠ approver, satu aktif per versi, stale-check atomik, applied_report_id same-shift, approved → versi baru + ack ulang.
5. Harga server-resolved saat buka shift; exclusion constraint; snapshot.
6. Chaining + rollover (modulus + threshold + maks 1 rollover) + reset (approver ≠ actor, effective_shift_id); delta negatif non-rollover ditolak.
7. Isolasi: tenant FK komposit + RLS; negative tests semua permukaan lolos CI.
8. Kebijakan immutable-versi + snapshot; tidak retroaktif; SPBU > org.
9. UI Bahasa Indonesia penuh.
10. Printout = layar.
11. audit_log append-only (owner role terpisah + SECURITY DEFINER + REVOKE; failed-request audit tidak ikut rollback).
12. Maks 1 shift open/awaiting per SPBU; needs_correction tidak memblokir; scheduler 24 jam dedup + clear + re-alert.
13. Backfill: flag + original_event_date + Owner approval + idempotency (UNIQUE org+station+event_date+shift_ke); meter chaining aman.
14. Submit atomik: lock draft (optimistic revision) → snapshot semua child → policy snapshot → status; idempotency key submit; concurrent submit ditolak.

## 8. Teknologi

Next.js + TypeScript + Postgres (Drizzle). btree_gist exclusion constraints. Deploy: VPS/vercel + managed Postgres.
Testing: TDD. Semua kode di git.

## 9. Fase

- F1: skema penuh (FK komposit, exclusion, RLS, audit role) + auth + permission evaluator + negative test matrix.
- F2: master data + harga/mapping + draft + lifecycle + chaining/rollover/reset + policy versioning + submit atomik idempoten.
- F3: amendment state machine + re-ack + break-glass + audit trail.
- F4: rekonsiliasi dual-unit + anomali + alert scheduler.
- F5: ack queue + backfill + laporan + printout + pengaturan.
- F6: pilot SPBU keluarga.

## 10. Runbook Pilot (gate F6)

- failed shift recovery: status failed dengan TIDAK ADA report → jalur REOPEN (prosedur): failed → open (draft kembali editing, revision++); amendment flow HANYA untuk shift yang punya report. Shift needs_correction tanpa report = tidak mungkin (guard di prosedur).
- gate matrix format (tiap baris: workflow, uji wajib, pass threshold, bukti test/artefak, rollback action):
| Workflow | Uji wajib | Pass threshold | Rollback |
|---|---|---|---|
| Shift normal (open→ack→lock) | 12 shift penuh, 0 tanpa ack | 100% shift ter-lock dgn ack | kembali ke Excel |
| Amend + re-ack | 1 siklus penuh | versi baru + ack ulang terekam | - |
| Break-glass | 1 aksi ber-alasan, muncul di anomali | ter-audit + muncul | - |
| Rollover/reset | 1 reset event ber-approval | chaining benar setelah reset | - |
| Evidence wajib & opsional | 1 submit ditolak; 1 exception ter-audit | keduanya | - |
| Harga/mapping berubah | 1 perubahan antar-shift, snapshot benar | expected sale konsisten | - |
| Alert starvation | 1 shift > 24 jam → alert idempoten | 1 occurrence, retry tidak dobel | - |
| Isolasi | negative-test matrix hijau di CI (repeat 3x) | 0 pelanggaran | block deploy |
| Backfill | 1 backfill idempoten (retry 3x) | retry = 1 report | freeze-submit, replay manual |
| Backup/restore | 1 restore sukses (RTO < 1 jam, RPO ≤ 5 menit via WAL) | data identik (checksum) | restore + replay WAL |
| Konkurensi (baru) | takeover race, ack supersession, pointer isolation, policy tombstone, audit outage, reset baseline, alert clear (masing-masing ≥3 iterasi) | semua sesuai §4.2 gate | block deploy |
| JWT rotasi (baru) | signature invalid, iss/aud/iat/exp/alg/kid/jti salah masing-masing 1 test + logout + retired key | 100% ditolak kecuali kasus grace | - |
| Invalid transition (baru) | setiap transisi ilegal di state machine ditolak | 100% | - |
| Audit replay (baru) | export audit trail + verifikasi hash berantai | 0 gap | - |
- Metrik: varian dual-unit terekam 100%; waktu input/shift < 10 menit; baseline Excel 1 minggu pembanding.
- Prosedur: backup harian; fallback kertas; rollback = kembali ke Excel (data aplikasi tetap).
- Sign-off: Owner sebelum SPBU kedua.

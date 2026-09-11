# PLAN.md — Aplikasi Rekonsiliasi SPBU (nama TBD)

Status: draft v11 (BUILD TARGET — resolusi review putaran 9; loop maks 25 putaran)
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

- Konvensi identitas: SEMUA ID = UUID v4 global-unique (dibuat server). FK komposit tenant: (org_id, station_id, id) parent key di SEMUA tabel ber-scope SPBU; FK child selalu (org_id, station_id, parent_id). Tabel org-scope: (org_id, id). Master data juga membawa org_id (+station_id bila scoped). Matrice DDL lengkap ditulis di ERD/migrasi; CI men-cek setiap FK membawa kolom tenant (lint otomatis pada file migrasi).
- RLS + authorization layer; direct base-table SELECT juga di-REVOKE dari role aplikasi — akses data hanya via view ber-RLS + prosedur. Default privileges diset utk role masa depan. SECURITY DEFINER procedures menjalankan set_config row_security=off secara eksplisit dan men-cek tenant di dalam prosedur.

### 4.1 Master data
- organizations, stations (timezone, versi: timezone dapat berubah; shift menyimpan snapshot timezone)
- nozzles (meter_max numeric(10,1) = nilai tampilan maksimum; modulus efektif = meter_max + 0.1 karena counter berhenti di 99999.9 lalu ke 0.0; meter decimal(10,1))
- nozzle_tank_map (nozzle, tank, tstretch tstzrange; EXCLUDE USING gist non-overlap)
- dispenser_prices (nozzle, harga integer, tstretch tstzrange, created_by; exclusion non-overlap)

### 4.2 Operasional
- shifts (org_id, station_id, supervisor_id, opened_at, closed_at, timezone_snapshot, business_date, status enum: open|submitting|failed|awaiting_confirmation|needs_correction|locked; backfilled boolean, original_event_date nullable, backfill_status: none|pending_approval|approved, backfill_approver, backfill_approved_at, backfill_reason, shift_ke NOT NULL, shift_price_map_snapshot jsonb NOT NULL + snapshot_hash, current_report_id nullable FK; partial UNIQUE(org_id, station_id, original_event_date, shift_ke) WHERE backfilled AND original_event_date IS NOT NULL)
- shift_drafts (org_id, station_id, shift_id, owned_by, claim_token uuid, claim_expires_at timestamptz, status: editing|submitting|submitted|failed|recovering, updated_by, updated_at, revision integer; UNIQUE(shift_id) — satu draft per shift SEUMUR HIDUP; claim = UPDATE WHERE status='editing' AND (claim_expires_at IS NULL OR claim_expires_at < now()) SET claim_token+expiry (lease 5 menit); recovery job: submitting > 10 menit → recovering → editing/failed; failed → retry = status editing dengan revision++ ; setiap mutasi child menaikkan revision) + TABEL DRAF ANAK (semua membawa org_id+station_id, FK komposit ke shift_drafts):
  - draft_readings (draft_id, nozzle_id, meter_start, meter_end, created_by; UNIQUE(draft_id, nozzle_id))
  - draft_sales (draft_id, dispenser_id, cash_amount, cashless_amount, created_by; UNIQUE(draft_id, dispenser_id))
  - draft_losses (draft_id, loss_id immutable PK (= loss_logical yang dipromosikan), direction, reason_code, liters, cash_amount, note, created_by)
  - draft_evidence_staging (draft_id, loss_id FK draft_losses, status: uploaded|verified|finalized, object_key, content_hash, size, mime, uploaded_by)
- submit_idempotency (org_id, station_id, shift_id, idempotency_key, request_hash, status: in_progress|succeeded|failed, claim_token (regenerated saat takeover; fencing utk update akhir), attempt_count, resulting_report_id nullable, error_detail nullable, created_at, updated_at; UNIQUE(org_id, station_id, shift_id, idempotency_key)). PROTOCOL: (1) INSERT ... ON CONFLICT DO NOTHING; kalau menang = pemilik submit (claim_token diset). kalau kalah = baca baris: succeeded → kembalikan report lama (re-cek otorisasi); in_progress + < 5 menit → 409 "sedang diproses"; in_progress + ≥ 5 menit → TAKEOVER: UPDATE ... WHERE status='in_progress' AND updated_at < now()-5min SET claim_token=baru, attempt_count++ RETURNING — jika UPDATE gagal (0 rows) = orang lain menang, 409. failed → retry dengan request_hash baru (claim_token baru). (2) hash mismatch = 409. (3) Finalisasi: UPDATE SET status='succeeded', resulting_report_id WHERE id AND claim_token = milikku — fencing token mencegah worker lama menimpa; FAILED update juga claim-gated (worker lama tidak bisa menandai failed setelah diambil alih). (4) pembuatan report + set succeeded = SATU transaksi; set failed = transaksi terpisah. Canonical request hash = sha256(JSON kanonik: shift_id + draft revision + seluruh child rows terurut + idempotency_key).
- shift_reports (id = report_id UUID global PK; org_id, station_id, shift_id; UNIQUE(org_id, station_id, shift_id, version_no); parent key utk FK = UNIQUE(org_id, station_id, report_id); supersedes_report_id → FK (org_id, station_id, shift_id, supersedes_report_id) + trigger cek same-shift; TIDAK ADA kolom is_current — sumber kebenaran tunggal = shifts.current_report_id, FK komposit (org_id, station_id, shift_id, current_report_id) — report dari shift lain tidak mungkin; status: submitted|locked; submitted_by, submitted_at; policy_snapshot_id → policy_snapshots (satukan: policy_snapshots punya snapshot_set_id; report menunjuk snapshot_set — lihat bawah); versi fisik loss/evidence per version_no)
- policy_snapshots: snapshot_set model — policy_snapshot_sets (org_id, station_id, shift_id, set_id PK, created_at) + policy_snapshot_items (set_id, policy_kind: threshold|evidence, policy_id, rev_id, scope, canonical payload jsonb, hash sha256; UNIQUE(set_id, policy_kind)). shift_reports.policy_snapshot_set_id → snapshot_set. Threshold + evidence keduanya terikat via satu set. Tombstone: resolusi henti pada revisi disabled → aturan tidak berlaku.
- dispenser_readings (org_id, station_id, shift_report_id, nozzle_id, meter_start, meter_end, price_used, expected_sale_rupiah, created_by, observed boolean default true, is_carried_forward boolean default false, source_shift_report_id nullable FK, source_reading_row_id nullable FK (wajib bila is_carried_forward); UNIQUE(shift_report_id, nozzle_id))
- sales_declared (org_id, station_id, shift_report_id, dispenser_id, cash_amount REQUIRED, cashless_amount REQUIRED default 0, created_by)
- loss_identity: tabel pemisah loss_logical (org_id, station_id, loss_id PK global; dibuat sekali per klaim) + loss_entries versi (org_id, station_id, shift_report_id, loss_logical_id FK, row_id PK; UNIQUE(shift_report_id, loss_logical_id)); amendment men-target loss_logical_id; evidence_event scoped (org_id, station_id, shift_report_id, loss_logical_id, loss_row_id) — snapshot evidence per report version immutable; event per loss punya event_seq integer + legal transition (uploaded→verified→finalized) + per-loss lock saat insert event (prosedur).
- loss_exception (org_id, station_id, loss_id FK loss_logical, reason, created_by; INSERT-only; dilarang saat policy wajib — prosedur + trigger)
- meter_reset_events (org_id, station_id, nozzle FK komposit, old_value (harus = meter_end laporan locked terakhir nozzle itu — di-cek prosedur), new_value, effective_shift_id FK komposit, reason, actor, approver ≠ actor, approved_at, status pending|approved; UNIQUE(org_id, station_id, nozzle, effective_shift_id) WHERE status='approved'; URUTAN: reset hanya dibuat jika nozzle punya ≥1 laporan locked ATAU via baseline awal eksplisit saat provisioning; reset approval ikut station lock yang sama dgn open/submit/backfill — diserialisasi per station+nozzle)
- amendments (org_id, station_id, base_report_id FK, reason, status pending|approved|rejected|superseded, requester, approver, decided_at, rejection_reason, applied_report_id FK same-shift (org_id, station_id, shift_id sama dengan base), stale_check_hash, is_break_glass + break_glass_reason nullable; partial UNIQUE(base_report_id) WHERE status='pending'; approval = transaksi atomik: lock shift+current report → verifikasi stale_check_hash → clone penuh report snapshot (semua child rows) → terapkan hanya allowlist → recompute anomali/evidence → set applied_report_id). Items: amendment_items (amendment_id, org_id, station_id, shift_id, target_kind enum, target_logical_id, field, old_value, new_value).
- ack_decisions: APPEND-ONLY penuh. ack_decisions (id PK, org_id, station_id, shift_id, report_id, version_no; FK komposit (org_id, station_id, shift_id, report_id) → shift_reports + prosedur cek version match; ack_seq integer, decision: acked|rejected, actor, decided_at, rejection_reason, is_superadmin, is_break_glass, break_glass_reason). ack_supersessions (id PK, superseded_ack_id FK, supersedes_by_ack_id FK, reason, created_at) — INSERT-only. TIDAK ADA kolom mutable. aktif = ack tanpa supersession row. ack_seq dialokasikan di prosedur dgn shift-row lock; tepat SATU aktif ditegakkan prosedur + deferred trigger. Terminologi: event_type vocabulary = fired|cleared (konsisten di semua tabel alert).
- alert_events (append-only penuh): event_id UUID PK, org_id, station_id, alert_key, period_start timestamptz, occurrence_number, event_type: fired|cleared (satu istilah: cleared), source_event_id FK nullable (khusus event cleared; FK komposit tenant), created_at. Unique rules: UNIQUE(org_id, station_id, alert_key, occurrence_number) WHERE event_type='fired'; UNIQUE(source_event_id) WHERE event_type='cleared' (satu cleared per fired). Alokasi occurrence: lock parent (alert_rules row, FOR UPDATE) lalu MAX(fired)+1 dalam transaksi yang sama.
- audit FAILED-request durability: denied request ditulis ke tabel audit_denied (append-only, transaksi TERPISAH via koneksi pool berbeda — bukan outbox yang ikut rollback). audit_outbox hanya utk event yang sukses.
- amendments: SKEMA TYPED (satu definisi saja — definisi target_table/target_id lama DIHAPUS). amendment_items: (amendment_id, org_id, station_id, shift_id, target_kind enum: sales_declared|loss_entry|delivery|dip_reading, target_logical_id, field, old_value, new_value) — setiap target_kind punya FK/prosedur-cek ke tabelnya (org+station+shift sama dengan base report). loss identity: loss_id = LOGICAL ID yang dipertahankan saat clone (row fisik baru = report version baru; loss_id lama tetap menunjuk entitas logis yang sama); evidence di-clone sebagai evidence_event baru yang menunjuk loss_id logis yang sama. DO/dip referensi: target_kind khusus dengan FK ke deliveries/dip_readings.
- deliveries (id PK, org_id, station_id, shift_id, do_number, tank, liters, created_by; FK komposit ke shifts) dan dip_readings (id PK, org_id, station_id, shift_id, tank, dip_liters, created_by; FK komposit ke shifts) — snapshot tables per report version: delivery_snapshots & dip_snapshots (shift_report_id FK, delivery_id/dip_id FK sumber, same-shift FK komposit). Carried-forward reading source FK: (org_id, station_id, shift_id, source_shift_report_id, source_reading_row_id) FK komposit + prosedur cek: source = predecessor locked report terakhir utk nozzle sama.
- numeric types: meter numeric(10,1) dengan CHECK 0≤nilai≤meter_max; volume numeric(8,2) CHECK ≥0; uang numeric(14,0) (integer rupiah, max 100 triliun) CHECK ≥0 utk cash/cashless/price; semua perhitungan numeric (bukan float); boundary tests: half-up .5, meter_max, overflow pengali, JSON/TS konversi (string di JSON, hindari float).
- alert_rules (id PK, org_id, station_id, rule_type, alert_key UNIQUE per rule (derived: rule_type + scope), threshold, channel, enabled, created_by). occurrence lock = FOR UPDATE pada alert_rules row. alert_events.source_event_id FK komposit tenant + CHECK: cleared wajib source fired dgn alert_key sama; CHECK (event_type='fired' AND source_event_id IS NULL) OR (event_type='cleared' AND source_event_id IS NOT NULL).
- baseline awal: nozzle_baseline (org_id, station_id, nozzle_id, initial_meter_value, created_by, approved_by Owner, created_at; UNIQUE(nozzle_id)) — provisioning-time; chaining generator: prioritas = meter_end locked report terakhir > reset approved > baseline; reset without prior locked report BOLEH hanya jika baseline ada.
- active-shift unique index KONKRET: partial UNIQUE INDEX ON shifts(org_id, station_id) WHERE status IN ('open','submitting','failed','awaiting_confirmation') — satu policy untuk semua status aktif; failed shift auto-abandon > 24 jam → needs_correction (tidak mengunci SPBU).
- gate matrix tambahan (concurrency/integrity tests): stale idempotency takeover race (2 worker, 1 pemenang); ack supersession (lama non-aktif setelah amendment); pointer FK isolation (report dari shift lain ditolak); policy tombstone (aturan disabled → aturan tak berlaku); audit outage (fail-closed, request ditolak); reset baseline ordering (reset tanpa locked report & tanpa baseline = ditolak); alert source uniqueness (2 clear utk 1 fired = ditolak).
- shift_transitions (org_id, station_id, shift_id, from_status, to_status, actor, at, reason) — riwayat status untuk alert starvation & audit
- Actor identity: DIPUTUS (TUNGGAL) — signed session JWT. Klaim: iss (tetap), sub (user_id), jti (unik, untuk revoke), iat/exp (exp 15 menit, clock skew toleransi 60 detik), aud (tetap: "spbu-recon"), alg PINNED HS256 (menolak alg lain/no alg). Key: satu secret aktif + prev secret saat rotasi (kid menunjuk key; rotasi dua fase). Revoke: tabel sessions (jti PK, revoked_at nullable; cek fn). Prosedur VERIFIKASI dulu token (signature+exp+jti blacklist+aud+alg) SEBELUM memakai identity; identity lookup tenant/station membership di dalam prosedur; klien TIDAK PERNAH mengirim actor_id terpisah. Failed request: identity dari token bila verifikasi signature lolos (walau otorisasi gagal), else anonymous+IP. Raw JWT TIDAK disimpan di audit (hanya sub + jti).
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
- PROTEKSI DML (matrice grants eksplisit): REVOKE ALL (termasuk SELECT) dari PUBLIC & role aplikasi pada SEMUA tabel: shifts, shift_drafts (+children), shift_reports, dispenser_readings, sales_declared, loss_logical/loss_entries/loss_exception, evidence_event, amendments, ack_decisions, ack_supersessions, shift_transitions, alert_rules, alert_events, threshold/evidence policies, policy_snapshot_sets/items, submit_idempotency, meter_reset_events, nozzle_baseline, dispenser_prices, nozzle_tank_map, deliveries, dip_readings, delivery_snapshots, dip_snapshots, sessions, audit_denied, audit_outbox, outbox_relay_state, organizations, stations, nozzles, dispensers, tanks, users, user_station_roles. SETIAP tabel baru wajib masuk matriks (CI cek: tabel tanpa klasifikasi privilege = gagal build). Relay role: UPDATE outbox_relay_state ONLY. Grant baca hanya pada view ber-RLS; grant tulis hanya prosedur.
- current-pointer invariant: shifts.current_report_id NULL sebelum ada report; non-NULL & valid (submitted/locked, same tenant+shift) saat ada report; pointer FK komposit (org_id, station_id, shift_id, current_report_id) → parent key UNIQUE(org_id, station_id, shift_id, report_id) pada shift_reports. Deferred constraint trigger men-cek invariant di akhir transaksi.
- amendment state machine (AUTHORITATIVE, satu definisi — definisi duplikat dihapus): pending → approved (→ report baru + applied_report_id) | rejected (dgn reason) | superseded (amendment baru untuk versi sama). approved tidak mengubah report lama; setiap state change ter-audit. ACK state machine (AUTHORITATIF): report tanpa ack → acked (→locked) atau rejected (→needs_correction); ack aktif = tanpa supersession row; amendment approved → INSERT supersession → report baru awaiting_confirmation.
- loss_entries versi: fields lengkap (row_id PK, org_id, station_id, shift_report_id FK komposit (org_id, station_id, shift_id, report_id), version_no, loss_logical_id FK, nozzle_id nullable, direction, reason_code, liters, cash_amount, note, created_by; UNIQUE(shift_report_id, loss_logical_id)). loss_exception scoped PER REPORT VERSION: (org_id, station_id, shift_report_id, loss_logical_id). evidence_event: PK evidence_id; kolom (org_id, station_id, shift_report_id, loss_logical_id, loss_row_id FK, event_seq, event_type, object_key, content_hash, size, mime, actor, at); UNIQUE(loss_row_id, event_seq); legal transitions (uploaded→verified→finalized) di prosedur; snapshot per report version immutable; hanya append-procedure insert.
- rollover config (rollover_threshold) ikut shift snapshot saat open.
- Lifecycle lengkap: shift status enum = open|submitting|failed|awaiting_confirmation|needs_correction|locked. Transisi: open → submitting → (sukses → awaiting_confirmation | gagal → failed → retry → open); awaiting_confirmation → (acked → locked | rejected → needs_correction); needs_correction → amendment approved (report status submitted atau locked) → versi baru → awaiting_confirmation. Unique aktif-shift mencakup open|submitting|failed|awaiting_confirmation.
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

Gate matrix (owner: Fadhlan; setiap gate butuh bukti test + pass threshold + rollback action):
| Workflow | Uji wajib |
|---|---|
| Shift normal (open→ack→lock) | 12 shift penuh, 0 tanpa ack |
| Amend + re-ack | 1 siklus penuh |
| Break-glass | 1 aksi ber-alasan, muncul di anomali |
| Rollover/reset | 1 reset event ber-approval |
| Evidence wajib & opsional | 1 submit ditolak; 1 exception ter-audit |
| Harga/mapping berubah | 1 perubahan antar-shift, snapshot benar |
| Alert starvation | 1 shift > 24 jam → alert idempoten |
| Isolasi | negative-test matrix hijau di CI |
| Backfill | 1 backfill idempoten |
| Backup/restore | 1 restore sukses |
- Metrik: varian dual-unit terekam 100%; waktu input/shift < 10 menit; baseline Excel 1 minggu pembanding.
- Prosedur: backup harian; fallback kertas; rollback = kembali ke Excel (data aplikasi tetap).
- Sign-off: Owner sebelum SPBU kedua.

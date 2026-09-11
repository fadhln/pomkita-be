# PLAN.md — Aplikasi Rekonsiliasi SPBU (nama TBD)

Status: draft v2 (alamat masalah review Codex)
Bahasa UI: Seluruh copywriting wajib Bahasa Indonesia (ID). Dokumen teknis memakai ASD-STE100 (Simplified Technical English).

## 1. Tujuan

Aplikasi multi-vendor untuk pelaporan dan monitoring operasi SPBU per shift.
Fokus MVP: pencegahan fraud pada pencatatan losses/gains dan penjualan.
Latar belakang: kerugian IDR 200 juta karena supervisor mencatat 100L sebagai losses padahal kehilangan uang. Tujuan utama: setiap klaim losses/gains punya bukti, alasan, dan persetujuan dari pihak independen.

## 2. Ruang Lingkup MVP

Termasuk:
- 1 organisasi, N SPBU (mulai dari 1, milik keluarga).
- Input manual (tanpa integrasi dispenser/POS).
- Rekonsiliasi per dispenser (nozzle-level), diagregasi ke shift.
- Expected sale = (meter_end − meter_start) × harga per dispenser.
- Laporan shift immutable + amendment + audit trail.
- Threshold configurable + daftar anomali + printout.

Ditunda (post-MVP):
- Rekonsiliasi kas (opening float, pengeluaran, setor) — v1 fokus volume→Rupiah.
- Acknowledgement meter oleh Operator.
- Notifikasi (email/WA) — alert hanya in-app.
- Integrasi Tokico/dispenser, koreksi suhu.
- Multi-organisasi.

## 3. Peran, Izin, dan Pemisahan Tugas

Prinsip: **pembuat data ≠ pengesah data**. Aturan ini ditegakkan di server.

| Peran | Cakupan | Kewenangan MVP |
|---|---|---|
| Operator | 1 SPBU | Akun saja; aksi ditunda |
| Supervisor | 1 SPBU | Mulai/selesai shift; input dispenser reading, sales, losses/gains, DO, dip; printout; ajukan amendment |
| Admin SPBU | 1..N SPBU | **Acknowledgement shift** (mengesahkan data shift); approve amendment; generate_report; view_all_stations; kelola dispenser/harga (flag) |
| Owner | Organisasi | Buat user; approve amendment; lihat audit trail, laporan, anomali; ubah threshold |
| Superadmin | Semua | Tanpa batas, **setiap aksi tercatat di audit log dengan penanda superadmin** |

Aturan pemisahan tugas (server-enforced):
1. Supervisor yang menginput TIDAK BOLEH meng-ack shift sendiri. Ack hanya oleh Admin SPBU/Owner yang berbeda user_id.
2. Supervisor yang mengajukan amendment TIDAK BOLEH menyetujuinya. Requester ≠ approver.
3. Satu amendment aktif per versi laporan (optimistic lock); penolakan wajib beralasan.
4. Acknowledgement memakai user_id + timestamp; tidak bisa dilakukan via Supervisor account.
5. Owner tidak menginput data operasional; hanya menyetujui/meninjau.

Permission flag (coarse): `ack_shift`, `approve_amendment`, `manage_users`, `generate_report`, `view_all_stations`, `manage_dispensers`.

## 4. Entitas (ringkas; ERD lengkap terpisah)

- organizations, stations, tanks
- users, user_station_roles (role + permissions jsonb)
- **dispensers** (station, kode, product_type: Pertamina/Pertamax/Solar, harga per liter)
- **dispenser_prices** (dispenser, harga, effective_from, created_by) — harga dapat berubah, riwayat tersimpan
- shifts (station, supervisor, opened_at, closed_at, business_date, status: open | awaiting_confirmation | locked)
- shift_reports (immutable, versi: draft | submitted | locked)
- **dispenser_readings** (shift_report, dispenser, meter_start, meter_end, price_used, expected_sale)
- loss_entries (direction: loss|gain, reason_code, liters, cash_amount terpisah, note, requires_ack)
- loss_evidence (photo/file/reference_no)
- deliveries (DO), dip_readings (per tank, link shift)
- amendments (before/after jsonb, status pending|approved|rejected, requester, approver)
- alert_rules, alert_events, alert_acks
- audit_log (append-only)

Aturan baku:
1. Tidak ada UPDATE pada shift_report_entries. Perubahan = versi baru + amendment.
2. Uang dan liter dicatat di kolom terpisah. Uang = integer rupiah (tanpa desimal).
3. Angka mentah hanya di shift_report_entries; varian/alert/laporan dihitung ulang.
4. Shift lintas tengah malam memakai business_date.
5. User tidak pernah dihapus permanen (soft delete).
6. Draft TIDAK masuk ledger. Ledger hanya menerima versi yang sudah disubmit. Draft yang belum disimpan = tidak ada jejak.
7. Rekonsiliasi per shift, dihitung dari detail per dispenser:
   - expected_sale_shift = Σ per dispenser: (meter_end − meter_start) × price_used
   - varian_shift = Σ sales_declared − expected_sale_shift (selain losses/gains yang dijelaskan)
   - loss/gain liter per dispenser juga dicatat (DIP-aware di fase berikutnya).

## 5. Aturan Rekonsiliasi dan Alert

Threshold default (configurable Admin/Owner per org/SPBU):
- Losses/gains per shift: 10L (standar Pertamina), nilai absolut (loss maupun gain).
- Selisih expected vs. declared sale: > 0 dianggap anomali, tercatat.

Perilaku di atas threshold:
- Prompt wajib: komentar + evidence + acknowledgement oleh Admin SPBU.
- Loss entry tanpa evidence tidak boleh disubmit.
- Alert event masuk daftar anomali Owner.

Kebijakan evidence (configurable Admin/Owner, per SPBU):
- Mode `opsional` (default): evidence diminta tapi boleh dikosongkan.
- Mode `wajib`: entry di atas threshold tidak dapat disubmit tanpa evidence (foto meter/kas, foto dokumen DO, atau nomor referensi).
- Jenis evidence yang diterima per aturan juga configurable (foto wajib vs. referensi cukup).

Perilaku di bawah threshold: tercatat normal, tanpa paksaan evidence.

## 6. Isolasi Multi-Tenant

- Setiap baris data operasional membawa org_id + station_id.
- Scoping ditegakkan di satu lapisan data-access (bukan per-query), berdasarkan user_station_roles.
- Negative tests wajib: akses lintas org dan lintas SPBU harus gagal (otomatis di CI).

## 7. Layar (screens)

1. Login
2. Dashboard Supervisor: shift aktif, Mulai/Akhiri Shift, ringkasan varian per dispenser
3. Form Input Shift: per dispenser (meter awal/akhir, harga otomatis), penjualan declared, losses/gains (+ reason, evidence di atas threshold)
4. Input DO + Dip Reading, link ke shift aktif
5. Daftar Amendment + form ajukan; antrean approval (Admin/Owner)
6. **Antrean Acknowledgement (Admin SPBU): shift awaiting_confirmation → ack → locked**
7. Daftar Anomali (Owner/Admin)
8. Laporan + filter + printout
9. Manajemen User + Dispenser/Harga
10. Pengaturan Threshold & Alert (opt-in, configurable)
11. Audit Trail (read-only)

## 8. Acceptance Criteria (MVP)

1. Supervisor dapat membuka/menutup shift lintas tengah malam; business_date benar.
2. Shift tidak dapat ter-lock tanpa acknowledgement oleh Admin/Owner yang bukan Supervisor penginput.
3. Losses/gains > threshold tidak dapat disubmit tanpa komentar + evidence.
4. Setiap perubahan data shift = amendment + audit trail; requester tidak dapat menyetujui amendment sendiri.
5. Expected sale dihitung dari per-dispenser (meter delta × harga riwayat yang berlaku); harga berubah tercatat dengan effective_from.
6. Akses lintas organisasi/SPBU ditolak; negative test lolos di CI.
7. Threshold dapat diubah Admin/Owner; berlaku per SPBU.
8. Seluruh teks UI berbahasa Indonesia (termasuk pesan validasi dan kosong/empty state).
9. Laporan shift dapat dicetak dengan data sama seperti layar.
10. Aksi Superadmin semua tercatat di audit log.

## 9. Teknologi

Next.js + TypeScript + Postgres (Drizzle). Deploy awal: VPS/vercel + managed Postgres.
Testing: TDD — test dahulu, lihat gagal, lalu implementasi. Semua kode di git.

## 10. Fase

- F1: skema DB + migrasi + auth + role/permission + isolation layer + negative tests.
- F2: shift lifecycle + dispenser/readings + validasi threshold.
- F3: amendment + approval (requester≠approver) + audit trail.
- F4: rekonsiliasi per dispenser + agregasi shift + anomali + alert event.
- F5: acknowledgement queue + laporan + printout + pengaturan.
- F6: pilot SPBU keluarga, umpan balik iteratif.

# PLAN.md — Aplikasi Rekonsiliasi SPBU (nama TBD)

Status: draft untuk review
Bahasa UI: Seluruh copywriting wajib Bahasa Indonesia (ID). Dokumen teknis memakai ASD-STE100 (Simplified Technical English).

## 1. Tujuan

Aplikasi multi-vendor untuk pelaporan dan monitoring operasi SPBU per shift.
Fokus MVP: pencegahan fraud pada pencatatan losses/gains dan penjualan tunai.
Latar belakang: kerugian IDR 200 juta karena supervisor mencatat 100L sebagai losses padahal kehilangan uang. Tujuan utama: setiap klaim losses/gains harus punya bukti, alasan, dan persetujuan.

## 2. Ruang Lingkup MVP

Termasuk:
- 1 organisasi, N SPBU (mulai dari 1, milik keluarga).
- Input manual (tidak ada integrasi dispenser/POS).
- Peran: Operator (akun saja, aksi ditunda), Supervisor, Admin, Owner, Superadmin.
- Laporan shift immutable + amendment + audit trail.
- 3 aturan varian (lihat §5) dengan threshold configurable.
- Daftar anomali untuk Owner.
- Cetak laporan shift (printout offline).

Ditunda (post-MVP):
- Acknowledgement meter oleh Operator.
- Notifikasi (email/WA) — alert hanya in-app dulu.
- Editor RBAC penuh (permission flag coarse saja).
- Integrasi Tokico/dispenser, koreksi suhu volume.
- Multi-organisasi.

## 3. Peran dan Izin

| Peran | Cakupan | Kewenangan MVP |
|---|---|---|
| Operator | 1 SPBU | Akun saja; aksi ditunda |
| Supervisor | 1 SPBU | Mulai/selesai shift; input data; DO; dip; printout; amendment (butuh approval) |
| Admin | 1..N SPBU | Sesuai flag: approve_amendment, override_shift, manage_users, generate_report, view_all_stations, edit_dip |
| Owner | Organisasi | Buat user, approve amendment, lihat audit trail, laporan, anomali |
| Superadmin | Semua | Tanpa batas |

Catatan penutupan shift: hanya Supervisor yang menutup setelah konfirmasi pembacaan akhir. Semua perubahan setelah close = jalur amendment.

## 4. Entitas (ringkas; ERD lengkap terpisah)

- organizations, stations, tanks
- users, user_station_roles (role + permissions jsonb)
- shifts (station, supervisor, opened_at, closed_at, business_date, status: open | awaiting_confirmation | locked)
- shift_reports (immutable, versi: draft | submitted | locked)
- shift_report_entries (meter_start, meter_end, sale_cash, sale_cashless, cash_counted, loss_liters_declared)
- loss_entries (direction: loss|gain, reason_code, liters, cash_amount terpisah, note, requires_ack)
- loss_evidence (photo/file/reference_no)
- deliveries (DO), dip_readings (per tank, link shift)
- amendments (before/after jsonb, status pending|approved|rejected)
- alert_rules, alert_events, alert_acks
- audit_log (append-only)

Aturan baku:
1. Tidak ada UPDATE pada shift_report_entries. Perubahan = versi baru + amendment.
2. Uang dan liter dicatat di kolom terpisah (cash_amount ≠ liters).
3. Angka mentah hanya di shift_report_entries; varian/alert/laporan dihitung ulang, tidak disimpan sebagai sumber kebenaran.
4. Shift yang melewati tengah malam memakai business_date, bukan tanggal kalender.
5. User tidak pernah dihapus permanen (soft delete) demi integritas audit.

## 5. Aturan Rekonsiliasi dan Alert

Threshold default (configurable oleh Admin/Owner per org atau SPBU):
- Losses/gains per shift: 10L (standar Pertamina).
- Selisih kas: 0 (setiap selisih tunai tercatat).
- Varian meter: closing ≠ opening + sales − losses yang dijelaskan.

Perilaku di atas threshold:
- Prompt wajib: komentar + dokumentasi (evidence) + acknowledgement.
- Loss entry tanpa evidence di atas threshold tidak boleh disubmit.
- Alert event dibuat dan masuk daftar anomali Owner.

Perilaku di bawah threshold: tercatat normal, tanpa paksaan evidence.

## 6. Layar (screens)

1. Login
2. Dashboard Supervisor: shift aktif, tombol Mulai/Akhiri Shift, ringkasan varian
3. Form Input Shift: meter awal/akhir, penjualan tunai/non-tunai, hitung kas, losses/gains (+ reason code, evidence saat threshold terlampaui)
4. Input Delivery Order (DO) + Dip Reading, link ke shift aktif
5. Daftar Amendment + form ajukan; antrean approval (Admin/Owner)
6. Daftar Anomali (Owner/Admin): shift diurutkan berdasar besar varian
7. Laporan + filter (per SPBU, business_date) + printout
8. Manajemen User (Owner/Admin dengan flag)
9. Pengaturan Threshold & Alert (opt-in, configurable)
10. Audit Trail (read-only)

## 7. Alur Utama

Buka shift (Supervisor) → input meter/penjualan/kas/losses → tutup shift (validasi varian; di atas threshold wajib evidence + ack) → shift status awaiting_confirmation → lock.
Amendment: ajukan dengan alasan + before/after → approval pemegang flag → versi baru shift_report + audit.
DO/Dip: masuk selama shift aktif, link ke shift.

## 8. Acceptance Criteria (MVP)

1. Supervisor dapat membuka dan menutup shift lintas tengah malam; business_date benar.
2. Losses > threshold tidak dapat disubmit tanpa komentar + evidence + acknowledgement.
3. Setiap perubahan data shift menghasilkan amendment + audit trail; tidak ada UPDATE langsung.
4. Selisih kas dan varian meter muncul otomatis di daftar anomali Owner.
5. Threshold dapat diubah oleh Admin/Owner dan berlaku per SPBU.
6. Seluruh teks UI berbahasa Indonesia.
7. Laporan shift dapat dicetak (printout) dengan data yang sama seperti di layar.
8. Owner hanya melihat data organisasinya; pemisahan data antar SPBU di tingkat query.

## 9. Teknologi

Next.js + TypeScript + Postgres (Drizzle), deploy awal sederhana (VPS/vercel + managed Postgres).
Testing: TDD, test dahulu sebelum implementasi. Semua kode di git.

## 10. Fase

- F1: skema DB + migrasi + auth + peran/permission flag.
- F2: shift lifecycle + form input + validasi threshold.
- F3: amendment + approval + audit trail.
- F4: rekonsiliasi (3 aturan) + daftar anomali + alert event.
- F5: laporan + printout + pengaturan threshold.
- F6: pilot di SPBU keluarga, umpan balik iteratif.

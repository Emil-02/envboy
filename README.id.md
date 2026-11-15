# envboy 🤠

**Backup, restore, cek, dan jaga semua file `.env` di semua repo kamu.** Satu binary Go, tanpa server, tanpa akun.

🇬🇧 [Read in English](README.md)

File `.env` memang sengaja tidak dimasukkan ke git. Dari sisi keamanan itu benar, tapi akibatnya setiap kali install ulang atau
ganti laptop kamu harus menyalin puluhan file satu per satu, entah lewat flashdisk, chat ke diri sendiri, atau skrip
`restore-env.sh` lama. Ada juga risiko secret tidak sengaja ikut ter-commit.

| Perintah | Fungsi |
|---|---|
| `envboy scan` | Mencari semua file env dan menandai yang **ter-commit** atau **belum masuk `.gitignore`** |
| `envboy backup` | Mengenkripsi semuanya ke **satu vault [age](https://age-encryption.org)** yang bisa disimpan di mana saja |
| `envboy restore` | Mengembalikan setiap file ke repo yang benar. Repo **dicocokkan lewat remote git**, jadi nama dan susunan folder boleh berbeda |
| `envboy status` | Menampilkan apa yang berubah sejak backup terakhir (hanya nama key, nilainya tidak pernah ditampilkan) |
| `envboy check` | Membandingkan `.env` dengan `.env.example`: key yang hilang, berlebih, atau kosong |
| `envboy guard` | Memasang hook pre-commit yang memblokir file env, dan memperbaiki `.gitignore` |
| `envboy` | Membuka dashboard interaktif |

## Instalasi

```sh
go install github.com/Emil-02/envboy@latest
```

Kamu juga bisa mengunduh binary dari [Releases](https://github.com/Emil-02/envboy/releases).

## Mulai cepat

```sh
envboy scan ~/Proyek            # lihat file env apa saja yang ada, dan apakah ada yang bocor
envboy backup ~/Proyek          # minta passphrase, lalu menulis ~/envboy.vault
# simpan ~/envboy.vault ke flashdisk, Google Drive, atau repo privat

# di laptop baru, setelah clone repo ke folder mana pun:
envboy restore ~/code --dry-run # lihat rencananya dulu
envboy restore ~/code           # tulis file-nya (ada konfirmasi)
```

## Enkripsi: passphrase atau kunci age

- **Passphrase (default):** kamu cukup mengingat satu kata sandi. Untuk skrip, isi variabel `ENVBOY_PASSPHRASE`.
- **Kunci age:** jalankan `envboy keygen` sekali. Setelah itu backup dan restore tidak lagi meminta passphrase. Vault juga bisa
  dibagikan ke anggota tim dengan `-r age1…`. Simpan file `~/.config/envboy/key.txt` baik-baik, karena tanpa file itu vault
  tidak bisa dibuka.

## Cara restore mencari repo yang benar

URL remote git dinormalisasi lebih dulu. Contohnya, `git@github.com:Me/App.git` dan `https://github.com/me/app` sama-sama
menjadi `github.com/me/app`.

1. Jika ada repo lokal dengan remote yang sama, file ditulis ke repo itu, di mana pun lokasinya.
2. Jika ada beberapa clone dengan remote yang sama, yang dipilih adalah repo dengan path relatif yang sama, lalu repo dengan
   nama folder yang sama. Jika tidak ada yang cocok, file dilewati dan ditandai *ambiguous*.
3. Repo tanpa remote dicocokkan lewat path relatif. Envboy tidak akan menulis ke repo lain yang kebetulan menempati path lama.

File lokal yang isinya berbeda dengan vault **tidak pernah ditimpa secara default**. Gunakan `--backup-existing` untuk
menyimpan file lama sebagai `.env.bak` dulu, atau `--force` untuk langsung menimpanya.

## Guard

```sh
envboy guard install --all ~/Proyek   # pasang hook pre-commit di semua repo
envboy guard fix ~/Proyek             # tambahkan aturan ignore yang belum ada
envboy guard fix --untrack            # sekaligus `git rm --cached` file env yang sudah ter-commit
```

Hook pre-commit yang sudah ada tetap dipertahankan dan dijalankan setelah pengecekan envboy.

## Kode keluar (untuk CI dan skrip)

| Kode | Arti |
|---|---|
| 0 | OK |
| 1 | Error, `check` menemukan key yang hilang, atau `guard run` memblokir commit |
| 2 | `scan` menemukan file env yang ter-commit |
| 3 | `status`: isi vault berbeda dengan file di disk |

Konfigurasi lengkap ada di [.envboy.example.yml](.envboy.example.yml). Model keamanannya dijelaskan di [SECURITY.md](SECURITY.md).

## Lisensi

[MIT](LICENSE)

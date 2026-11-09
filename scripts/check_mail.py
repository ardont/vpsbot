#!/usr/bin/env python3
"""
Fast & Intelligent IMAP Mailbox Inspector for SuperBot and Antigravity.
Optimized with parallel account querying via ThreadPoolExecutor.
Modes:
  1. Default (no args): Fast formatted HTML for Telegram buttons.
  2. --deep [--limit N]: Structured JSON with subject, sender, date, unread flag and body snippets.
  3. --read <account> <id>: Prints full decoded body of a specific email.
"""

import sys
import os
import re
import html
import json
import imaplib
import email
from email.header import decode_header
from concurrent.futures import ThreadPoolExecutor

def parse_env(filepath):
    env = {}
    if not os.path.exists(filepath):
        return env
    with open(filepath, 'r', encoding='utf-8') as f:
        for line in f:
            line = line.strip()
            if not line or line.startswith('#') or '=' not in line:
                continue
            k, v = line.split('=', 1)
            env[k.strip()] = v.strip().strip("'\"")
    return env

def decode_mime(header_val):
    if not header_val:
        return ""
    decoded_fragments = decode_header(header_val)
    res = []
    for fragment, encoding in decoded_fragments:
        if isinstance(fragment, bytes):
            if encoding:
                try:
                    res.append(fragment.decode(encoding))
                except Exception:
                    res.append(fragment.decode('utf-8', errors='replace'))
            else:
                res.append(fragment.decode('utf-8', errors='replace'))
        else:
            res.append(str(fragment))
    return "".join(res)

def extract_body(msg, max_len=None):
    body = ""
    if msg.is_multipart():
        for part in msg.walk():
            ctype = part.get_content_type()
            cdispo = str(part.get('Content-Disposition'))
            if ctype == 'text/plain' and 'attachment' not in cdispo:
                try:
                    payload = part.get_payload(decode=True)
                    charset = part.get_content_charset() or 'utf-8'
                    body = payload.decode(charset, errors='replace')
                    if body:
                        break
                except Exception:
                    pass
        if not body:
            for part in msg.walk():
                ctype = part.get_content_type()
                if ctype == 'text/html':
                    try:
                        payload = part.get_payload(decode=True)
                        charset = part.get_content_charset() or 'utf-8'
                        raw = payload.decode(charset, errors='replace')
                        body = re.sub(r'<[^>]+>', ' ', raw)
                        if body:
                            break
                    except Exception:
                        pass
    else:
        try:
            payload = msg.get_payload(decode=True)
            charset = msg.get_content_charset() or 'utf-8'
            body = payload.decode(charset, errors='replace')
            if msg.get_content_type() == 'text/html':
                body = re.sub(r'<[^>]+>', ' ', body)
        except Exception:
            pass

    clean = re.sub(r'[ \t]+', ' ', body).strip()
    if max_len and len(clean) > max_len:
        return clean[:max_len] + "..."
    return clean

def get_accounts():
    env = parse_env("/opt/superbot/.env")
    if not env:
        env = parse_env("/root/vpsbot/.env")

    return {
        "gmail": {
            "name": "Gmail",
            "emoji": "🔴",
            "host": "imap.gmail.com",
            "user": env.get("GMAIL_USER", ""),
            "pass": env.get("GMAIL_APP_PASSWORD", ""),
        },
        "mailru": {
            "name": "Mail.ru",
            "emoji": "🔷",
            "host": "imap.mail.ru",
            "user": env.get("MAILRU_USER", ""),
            "pass": env.get("MAILRU_APP_PASSWORD", ""),
        }
    }

def fetch_account_deep(key, acc, limit):
    user = acc["user"]
    pwd = acc["pass"]
    host = acc["host"]

    res = {
        "name": acc["name"],
        "user": user,
        "connected": False,
        "total_count": 0,
        "unread_count": 0,
        "emails": []
    }

    if not user or not pwd:
        res["error"] = "No credentials configured"
        return key, res

    try:
        mail = imaplib.IMAP4_SSL(host, 993, timeout=8)
        mail.login(user, pwd)
        status, _ = mail.select("INBOX", readonly=True)
        if status != "OK":
            res["error"] = f"Failed to select INBOX: {status}"
            mail.logout()
            return key, res

        res["connected"] = True
        _, unread_data = mail.search(None, "UNSEEN")
        unread_ids = unread_data[0].split() if unread_data and unread_data[0] else []
        res["unread_count"] = len(unread_ids)

        _, all_data = mail.search(None, "ALL")
        all_ids = all_data[0].split() if all_data and all_data[0] else []
        res["total_count"] = len(all_ids)

        unread_set = set(unread_ids)
        target_ids = unread_ids[-limit:] if unread_ids else all_ids[-limit:]

        for num in reversed(target_ids):
            status, data = mail.fetch(num, '(RFC822)')
            if status != "OK" or not data:
                continue
            raw_email = data[0][1]
            msg = email.message_from_bytes(raw_email)

            subject = decode_mime(msg.get("Subject", "(без темы)"))
            sender = decode_mime(msg.get("From", ""))
            date_str = msg.get("Date", "")
            snippet = extract_body(msg, max_len=300)

            res["emails"].append({
                "id": num.decode(),
                "date": date_str,
                "sender": sender,
                "subject": subject,
                "is_unread": num in unread_set,
                "snippet": snippet
            })

        mail.close()
        mail.logout()
    except Exception as e:
        res["error"] = str(e)

    return key, res

def run_deep_check(limit=6):
    accounts = get_accounts()
    results = {}

    with ThreadPoolExecutor(max_workers=2) as executor:
        futures = [executor.submit(fetch_account_deep, k, acc, limit) for k, acc in accounts.items()]
        for f in futures:
            k, res = f.result()
            results[k] = res

    print(json.dumps(results, ensure_ascii=False, indent=2))

def read_full_email(account_key, email_id):
    accounts = get_accounts()
    acc = accounts.get(account_key.lower())
    if not acc:
        print(f"Unknown account '{account_key}'. Use 'gmail' or 'mailru'.")
        return

    user = acc["user"]
    pwd = acc["pass"]
    host = acc["host"]

    try:
        mail = imaplib.IMAP4_SSL(host, 993, timeout=10)
        mail.login(user, pwd)
        mail.select("INBOX", readonly=True)
        status, data = mail.fetch(email_id.encode(), '(RFC822)')
        if status != "OK" or not data:
            print(f"Email ID {email_id} not found.")
            return

        msg = email.message_from_bytes(data[0][1])
        sender = decode_mime(msg.get("From", ""))
        subject = decode_mime(msg.get("Subject", ""))
        date_str = msg.get("Date", "")
        body = extract_body(msg, max_len=None)

        print(f"From: {sender}")
        print(f"Subject: {subject}")
        print(f"Date: {date_str}")
        print("-" * 50)
        print(body)

        mail.close()
        mail.logout()
    except Exception as e:
        print(f"Error reading email: {e}")

def fetch_account_html(key, acc, limit):
    name = acc["name"]
    emoji = acc["emoji"]
    host = acc["host"]
    user = acc["user"]
    pwd = acc["pass"]

    lines = [f"{emoji} <b>{name}</b> (<code>{html.escape(user)}</code>)"]
    if not user or not pwd:
        lines.append("└ ⚠️ <i>Учетные данные не заданы в .env</i>\n")
        return key, lines

    try:
        mail = imaplib.IMAP4_SSL(host, 993, timeout=8)
        mail.login(user, pwd)
        status, _ = mail.select("INBOX", readonly=True)
        if status != "OK":
            lines.append(f"└ ❌ <i>Не удалось открыть INBOX: {status}</i>\n")
            mail.logout()
            return key, lines

        status, unseen_data = mail.search(None, "UNSEEN")
        unseen_ids = unseen_data[0].split() if status == "OK" and unseen_data[0] else []

        status, all_data = mail.search(None, "ALL")
        all_ids = all_data[0].split() if status == "OK" and all_data[0] else []

        lines.append(f"├ <b>Всего писем:</b> <code>{len(all_ids):,}</code> | <b>Непрочитанных:</b> <code>{len(unseen_ids):,}</code>")

        target_ids = unseen_ids[-limit:] if unseen_ids else all_ids[-limit:]
        if not target_ids:
            lines.append("└ 📭 <i>Папка входящих пуста</i>\n")
            mail.logout()
            return key, lines

        lines.append("└ <b>Последние входящие:</b>")
        for num in reversed(target_ids):
            status, data = mail.fetch(num, '(RFC822.HEADER)')
            if status != "OK":
                continue
            raw_email = data[0][1]
            msg = email.message_from_bytes(raw_email)
            subject = decode_mime(msg.get("Subject", "(без темы)"))
            sender = decode_mime(msg.get("From", ""))
            date_val = msg.get("Date", "")

            is_new = "🔴 " if num in unseen_ids else "⚪ "
            if len(sender) > 40:
                sender = sender[:38] + "…"
            if len(subject) > 60:
                subject = subject[:58] + "…"

            clean_sender = html.escape(sender)
            clean_sub = html.escape(subject)
            clean_date = html.escape(str(date_val)[:25]) if date_val else ""

            lines.append(f"  {is_new}<b>{clean_sender}</b>\n    └ <i>{clean_sub}</i> <tg-spoiler>({clean_date})</tg-spoiler>")

        lines.append("")
        mail.close()
        mail.logout()
    except Exception as e:
        lines.append(f"└ ❌ <i>Ошибка подключения: {html.escape(str(e))}</i>\n")

    return key, lines

def run_standard_html(limit=4):
    accounts = get_accounts()
    out = ["<b>📬 Результаты проверки почтовых ящиков</b>\n"]

    results = {}
    with ThreadPoolExecutor(max_workers=2) as executor:
        futures = [executor.submit(fetch_account_html, k, acc, limit) for k, acc in accounts.items()]
        for f in futures:
            k, lines = f.result()
            results[k] = lines

    # Keep deterministic order: gmail first, then mailru
    for key in ["gmail", "mailru"]:
        if key in results:
            out.extend(results[key])

    out.append("<i>(Обозначения: 🔴 — непрочитанное, ⚪ — прочитанное)</i>")
    print("\n".join(out))

def main():
    args = sys.argv[1:]
    if "--deep" in args or "--json" in args:
        limit = 6
        if "--limit" in args:
            idx = args.index("--limit")
            if idx + 1 < len(args) and args[idx+1].isdigit():
                limit = int(args[idx+1])
        run_deep_check(limit=limit)
    elif "--read" in args:
        idx = args.index("--read")
        if idx + 2 < len(args):
            acc = args[idx+1]
            eid = args[idx+2]
            read_full_email(acc, eid)
        else:
            print("Usage: --read <gmail|mailru> <email_id>")
    else:
        run_standard_html(limit=4)

if __name__ == "__main__":
    main()

# -*- coding: utf-8 -*-
"""
把 docs/design/ 下的分册合订成一本 PDF。

用法：
    python tools/build-pdf.py

依赖：mistune（Markdown 渲染）、本机 Chrome/Edge（打印 PDF）。
产物：docs/OpenVow-设计文档-合订本.pdf

规则：
  * 按文件名排序，README 永远在最前；
  * 每册从新的一页开始；
  * 中间文件 docs/design/_book.html 可随时删除。
"""
import io
import os
import re
import shutil
import subprocess
import sys

import mistune

BASE = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
SRC_DIR = os.path.join(BASE, "docs", "design")
HTML = os.path.join(SRC_DIR, "_book.html")
PDF = os.path.join(BASE, "docs", "OpenVow-设计文档-合订本.pdf")

CSS = """
@page { size: A4 portrait; margin: 17mm 15mm 16mm; }
* { box-sizing: border-box; }
html, body { margin: 0; padding: 0; }
body {
  font-family: "Microsoft YaHei", "PingFang SC", "Noto Sans SC", sans-serif;
  font-size: 10.2pt; line-height: 1.72; color: #1b2230;
  -webkit-print-color-adjust: exact; print-color-adjust: exact;
}
.cover { height: 250mm; display: flex; flex-direction: column; justify-content: center;
         page-break-after: always; padding-left: 6mm; }
.cover .eyebrow { font-size: 9pt; letter-spacing: .3em; color: #0f8f83; text-transform: uppercase; margin-bottom: 14mm; }
.cover h1 { font-size: 30pt; margin: 0 0 8mm; letter-spacing: .02em; border: 0; padding: 0; }
.cover .sub { font-size: 12pt; color: #5b6680; line-height: 1.9; max-width: 120mm; }
.cover .meta { margin-top: 22mm; font-size: 9.5pt; color: #7c879d; line-height: 2; }
.cover .rule { width: 34mm; height: 3px; background: #0f8f83; margin: 10mm 0; }

.doc + .doc { page-break-before: always; }
h1 { font-size: 19pt; margin: 0 0 6mm; padding-bottom: 3mm; border-bottom: 2px solid #0f8f83;
     page-break-after: avoid; }
h2 { font-size: 14.5pt; margin: 9mm 0 3mm; padding-left: 3mm; border-left: 4px solid #0f8f83;
     page-break-after: avoid; }
h3 { font-size: 11.6pt; margin: 6mm 0 2mm; color: #22304a; page-break-after: avoid; }
h4 { font-size: 10.6pt; margin: 4mm 0 1.5mm; color: #33415c; page-break-after: avoid; }
p { margin: 0 0 2.6mm; text-align: justify; }
ul, ol { margin: 0 0 2.8mm; padding-left: 6mm; }
li { margin: 0 0 1.2mm; }
strong { color: #101827; }
a { color: #0f6f9c; text-decoration: none; }
hr { border: 0; border-top: 1px solid #e3e7ef; margin: 7mm 0; }
blockquote { margin: 3mm 0; padding: 2.5mm 4mm; background: #fffbf2;
             border-left: 3px solid #c8a24a; color: #4d5a72; }
blockquote p { margin: 0; }

code { font-family: Consolas, "Courier New", "Microsoft YaHei", monospace;
       background: #f1f4f9; border: 1px solid #e4e9f2; border-radius: 3px;
       padding: 0.5px 3px; font-size: 8.8pt; color: #24405a; }
pre { background: #f7f9fc; border: 1px solid #e3e7ef; border-radius: 5px;
      padding: 3mm 3.5mm; margin: 0 0 3mm; overflow-x: auto;
      page-break-inside: avoid; line-height: 1.55; }
pre code { background: none; border: 0; padding: 0; font-size: 8.1pt; color: #2a3348; }

.tw { margin: 0 0 3.5mm; page-break-inside: auto; }
table { width: 100%; border-collapse: collapse; font-size: 8.9pt; }
thead { display: table-header-group; }
tr { page-break-inside: avoid; }
thead th { background: #f1f4f9; color: #33415c; font-weight: 600; }
th, td { border: 1px solid #dfe5ef; padding: 1.6mm 2.2mm; vertical-align: top; text-align: left; }
tbody tr:nth-child(even) td { background: #fafbfd; }
td code, th code { font-size: 8.2pt; }
"""

TEMPLATE = """<!DOCTYPE html>
<html lang="zh-CN"><head><meta charset="utf-8">
<title>OpenVow 设计文档 合订本 v1.2</title><style>{css}</style></head>
<body>
<div class="cover">
  <div class="eyebrow">Neutral Task Broker</div>
  <h1>OpenVow 设计文档<br>合订本 v1.2</h1>
  <div class="rule"></div>
  <div class="sub">人定契约，终端干活，平台只做派发、校验、归集、合并。<br>
  没有隐含动作，只有任务；判卷的人不是考生。</div>
  <div class="meta">共 {count} 册 · 2026-10<br>适用 OpenVow 1.x</div>
</div>
{body}
</body></html>"""

CHROME_CANDIDATES = [
    r"C:\Program Files\Google\Chrome\Application\chrome.exe",
    r"C:\Program Files (x86)\Google\Chrome\Application\chrome.exe",
    r"C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe",
    r"C:\Program Files\Microsoft\Edge\Application\msedge.exe",
]


def find_browser():
    for p in CHROME_CANDIDATES:
        if os.path.exists(p):
            return p
    for name in ("chrome", "msedge"):
        p = shutil.which(name)
        if p:
            return p
    return None


def ordered_files():
    names = [n for n in os.listdir(SRC_DIR) if n.endswith(".md") and not n.startswith("_")]
    names.sort(key=lambda n: (0 if n == "README.md" else 1, n))
    return [os.path.join(SRC_DIR, n) for n in names]


def main():
    files = ordered_files()
    if not files:
        print("没有找到任何 md")
        return 1

    render = mistune.create_markdown(escape=False, plugins=["table", "strikethrough", "url"])

    sections = []
    for path in files:
        with io.open(path, "r", encoding="utf-8") as f:
            md = f.read()
        html = render(md)
        html = html.replace("<table>", '<div class="tw"><table>').replace("</table>", "</table></div>")
        sections.append('<section class="doc">\n%s\n</section>' % html)
        print("  +", os.path.basename(path))

    out = TEMPLATE.format(css=CSS, body="\n".join(sections), count=len(files))
    with io.open(HTML, "w", encoding="utf-8") as f:
        f.write(out)
    print("html ->", HTML)

    browser = find_browser()
    if not browser:
        print("未找到 Chrome/Edge，跳过 PDF。可用浏览器打开 %s 后手动打印。" % HTML)
        return 1

    if os.path.exists(PDF):
        os.remove(PDF)
    url = "file:///" + HTML.replace("\\", "/").lstrip("/")
    subprocess.run([browser, "--headless=new", "--disable-gpu", "--no-pdf-header-footer",
                    "--print-to-pdf=" + PDF, url],
                   check=False, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    if os.path.exists(PDF):
        print("pdf  ->", PDF, os.path.getsize(PDF), "bytes")
        try:
            os.remove(HTML)          # 中间文件不留在仓库里
        except OSError:
            pass
        return 0
    print("PDF 生成失败")
    return 1


if __name__ == "__main__":
    sys.exit(main())

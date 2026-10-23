"""Build a deterministic two-page invoice for PDF parsing, text and rendering tests."""
import pathlib
import zlib

objects = [b'<< /Type /Catalog /Pages 2 0 R >>',
           b'<< /Type /Pages /Kids [4 0 R 6 0 R] /Count 2 >>',
           b'<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>']
for page in range(2):
    lines = ['Wago Services - Invoice 2026-1013', f'Page {page + 1} of 2',
             'Customer: Example Manufacturing', 'Description                         Quantity     Amount']
    for row in range(20):
        item = page * 20 + row + 1
        lines.append(f'PDF processing batch {item:02d}              {item:2d}         {item * 125}.00')
    lines += ['Subtotal: 102500.00' if page else 'Continued on page 2',
              'Payment due: 2026-11-20']
    commands = ['BT /F1 11 Tf 42 750 Td 17 TL']
    for i, line in enumerate(lines):
        if i:
            commands.append('T*')
        commands.append(f'({line}) Tj')
    commands.append('ET')
    stream = zlib.compress('\n'.join(commands).encode(), 9)
    objects.append(f'<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 3 0 R >> >> /Contents {5 + page * 2} 0 R >>'.encode())
    objects.append(f'<< /Length {len(stream)} /Filter /FlateDecode >>\nstream\n'.encode() + stream + b'\nendstream')
pdf = bytearray(b'%PDF-1.7\n%\xe2\xe3\xcf\xd3\n')
offsets = [0]
for index, obj in enumerate(objects, 1):
    offsets.append(len(pdf))
    pdf.extend(f'{index} 0 obj\n'.encode() + obj + b'\nendobj\n')
xref = len(pdf)
pdf.extend(f'xref\n0 {len(objects)+1}\n0000000000 65535 f \n'.encode())
for offset in offsets[1:]:
    pdf.extend(f'{offset:010d} 00000 n \n'.encode())
pdf.extend(f'trailer\n<< /Root 1 0 R /Size {len(objects)+1} >>\nstartxref\n{xref}\n%%EOF\n'.encode())
pathlib.Path(__file__).with_name('invoice.pdf').write_bytes(pdf)

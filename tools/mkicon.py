#!/usr/bin/env python3
"""mkicon.py ICON.ico OUT.syso -- a Windows .ico as a COFF resource object (RT_ICON + RT_GROUP_ICON),
which the Go linker builds into the Windows exe: its icon in Explorer, the taskbar and Alt-Tab.
(The same job as akavel/rsrc, in a page of Python.)"""
import struct, sys

def main(ico_path, out_path):
    ico = open(ico_path, 'rb').read()
    _, typ, n = struct.unpack_from('<HHH', ico, 0)
    assert typ == 1, 'not an icon'
    imgs = []
    for i in range(n):
        w, h, cc, _, planes, bpp, size, off = struct.unpack_from('<BBBBHHII', ico, 6 + 16 * i)
        imgs.append((w, h, cc, planes, bpp, ico[off:off + size]))
    group = struct.pack('<HHH', 0, 1, n) + b''.join(
        struct.pack('<BBBBHHIH', w, h, cc, 0, planes, bpp, len(d), i + 1) for i, (w, h, cc, planes, bpp, d) in enumerate(imgs))
    # resources: (type, id, data); the directory tree is type -> id -> language
    res = [(3, i + 1, d[5]) for i, d in enumerate(imgs)] + [(14, 1, group)]
    types = sorted({t for t, _, _ in res})
    def dirhdr(nids):
        return struct.pack('<IIHHHH', 0, 0, 0, 0, 0, nids)
    # sizes: root + type dirs + name dirs (each 16 + 8*entries)
    root_sz = 16 + 8 * len(types)
    type_dirs = {t: [r for r in res if r[0] == t] for t in types}
    off = root_sz
    type_off = {}
    for t in types:
        type_off[t] = off; off += 16 + 8 * len(type_dirs[t])
    lang_off = {}
    for r in res:
        lang_off[(r[0], r[1])] = off; off += 16 + 8
    data_entry_off = {}
    for r in res:
        data_entry_off[(r[0], r[1])] = off; off += 16
    data_off = {}
    for r in res:
        off = (off + 7) & ~7
        data_off[(r[0], r[1])] = off; off += len(r[2])
    sec = bytearray(off)
    struct.pack_into('<16s', sec, 0, dirhdr(len(types)))
    for k, t in enumerate(types):
        struct.pack_into('<II', sec, 16 + 8 * k, t, 0x80000000 | type_off[t])
        b = type_off[t]
        struct.pack_into('<16s', sec, b, dirhdr(len(type_dirs[t])))
        for j, r in enumerate(type_dirs[t]):
            struct.pack_into('<II', sec, b + 16 + 8 * j, r[1], 0x80000000 | lang_off[(r[0], r[1])])
    relocs = []
    for r in res:
        k = (r[0], r[1])
        struct.pack_into('<16s', sec, lang_off[k], dirhdr(1))
        struct.pack_into('<II', sec, lang_off[k] + 16, 0x0409, data_entry_off[k])     # a leaf: no high bit
        struct.pack_into('<IIII', sec, data_entry_off[k], data_off[k], len(r[2]), 0, 0)
        relocs.append(data_entry_off[k])        # OffsetToData is an RVA: relocated against .rsrc
        sec[data_off[k]:data_off[k] + len(r[2])] = r[2]
    # COFF: header, one section header, the section, its relocations, one symbol, empty string table
    hdr_sz, sh_sz = 20, 40
    raw_ptr = hdr_sz + sh_sz
    rel_ptr = raw_ptr + len(sec)
    sym_ptr = rel_ptr + 10 * len(relocs)
    out = struct.pack('<HHIIIHH', 0x8664, 1, 0, sym_ptr, 1, 0, 0)
    out += struct.pack('<8sIIIIIIHHI', b'.rsrc', 0, 0, len(sec), raw_ptr, rel_ptr, 0, len(relocs), 0, 0x40000040)
    out += bytes(sec)
    out += b''.join(struct.pack('<IIH', r, 0, 3) for r in relocs)            # IMAGE_REL_AMD64_ADDR32NB
    out += struct.pack('<8sIhHBB', b'.rsrc', 0, 1, 0, 3, 0)                  # the section's symbol
    out += struct.pack('<I', 4)
    open(out_path, 'wb').write(out)

if __name__ == '__main__':
    main(sys.argv[1], sys.argv[2])

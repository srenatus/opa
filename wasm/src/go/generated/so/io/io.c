#include "io.h"

// -- Variables and constants --

// Discard is a [Writer] on which all Write calls
// succeed without doing anything.
io_Writer io_Discard = (io_Writer){.self = &(io_DiscardWriter){}, .Write = io_DiscardWriter_Write};

// Seek whence values.
const so_int io_SeekStart = 0;
const so_int io_SeekCurrent = 1;
const so_int io_SeekEnd = 2;

// EOF is the error returned by Read when no more input is available.
// (Read must return EOF itself, not an error wrapping EOF,
// because callers will test for EOF using ==.)
// Functions should return EOF only to signal a graceful end of input.
// If the EOF occurs unexpectedly in a structured data stream,
// the appropriate error is either [ErrUnexpectedEOF] or some other error
// giving more detail.
so_Error io_EOF = errors_New("EOF");

// ErrInvalidWrite means that a write returned an impossible count.
so_Error io_ErrInvalidWrite = errors_New("io: Write returned impossible count");

// ErrNegativeRead means that a read returned a negative count.
so_Error io_ErrNegativeRead = errors_New("io: Read returned negative count");

// ErrNoProgress is returned by some clients of a [Reader] when
// many calls to Read have failed to return any data or error,
// usually the sign of a broken [Reader] implementation.
so_Error io_ErrNoProgress = errors_New("io: multiple Read calls return no data or error");

// ErrOffset is returned by seek functions when the offset argument is invalid.
so_Error io_ErrOffset = errors_New("io: invalid offset");

// ErrShortBuffer means that a read required a longer buffer than was provided.
so_Error io_ErrShortBuffer = errors_New("io: short buffer");

// ErrShortWrite means that a write accepted fewer bytes than requested
// but failed to return an explicit error.
so_Error io_ErrShortWrite = errors_New("io: short write");

// ErrUnexpectedEOF means that EOF was encountered in the
// middle of reading a fixed-size block or data structure.
so_Error io_ErrUnexpectedEOF = errors_New("io: unexpected EOF");

// ErrUnread is returned by unread operations when they can't perform for some reason.
so_Error io_ErrUnread = errors_New("io: cannot unread previous read operation");

// ErrWhence is returned by seek functions when the whence argument is invalid.
so_Error io_ErrWhence = errors_New("io: invalid whence");

// -- faces.go --

// -- impls.go --

so_R_int_err io_DiscardWriter_Write(void* self, so_Slice p) {
    (void)self;
    return (so_R_int_err){.val = so_len(p), .err = NULL};
}

so_R_int_err io_DiscardWriter_WriteString(void* self, so_String s) {
    (void)self;
    return (so_R_int_err){.val = so_len(s), .err = NULL};
}

// LimitReader returns a LimitedReader that reads from r
// but stops with EOF after n bytes.
io_LimitedReader io_LimitReader(io_Reader r, int64_t n) {
    return (io_LimitedReader){r, n};
}

so_R_int_err io_LimitedReader_Read(void* self, so_Slice p) {
    io_LimitedReader* l = (io_LimitedReader*)self;
    if (l->N <= 0) {
        return (so_R_int_err){.val = 0, .err = io_EOF};
    }
    if ((int64_t)(so_len(p)) > l->N) {
        p = so_slice(so_byte, p, 0, l->N);
    }
    so_R_int_err _res1 = l->R.Read(l->R.self, p);
    so_int n = _res1.val;
    so_Error err = _res1.err;
    l->N -= (int64_t)(n);
    return (so_R_int_err){.val = n, .err = err};
}

// NewNopCloser returns a [NopCloser] wrapping r.
io_NopCloser io_NewNopCloser(io_Reader r) {
    return (io_NopCloser){r};
}

so_R_int_err io_NopCloser_Read(void* self, so_Slice p) {
    io_NopCloser* n = (io_NopCloser*)self;
    return n->r.Read(n->r.self, p);
}

so_Error io_NopCloser_Close(void* self) {
    (void)self;
    return NULL;
}

// NewSectionReader returns a [SectionReader] that reads from r
// starting at offset off and stops with EOF after n bytes.
io_SectionReader io_NewSectionReader(io_ReaderAt r, int64_t off, int64_t n) {
    int64_t remaining = 0;
    if (off <= INT64_MAX - n) {
        remaining = n + off;
    } else {
        // Overflow, with no way to return error.
        // Assume we can read up to an offset of 1<<63 - 1.
        remaining = INT64_MAX;
    }
    return (io_SectionReader){r, off, off, remaining, n};
}

so_R_int_err io_SectionReader_Read(void* self, so_Slice p) {
    io_SectionReader* s = (io_SectionReader*)self;
    if (s->off >= s->limit) {
        return (so_R_int_err){.val = 0, .err = io_EOF};
    }
    {
        int64_t max = s->limit - s->off;
        if ((int64_t)(so_len(p)) > max) {
            p = so_slice(so_byte, p, 0, max);
        }
    }
    so_R_int_err _res1 = s->r.ReadAt(s->r.self, p, s->off);
    so_int n = _res1.val;
    so_Error err = _res1.err;
    s->off += (int64_t)(n);
    return (so_R_int_err){.val = n, .err = err};
}

so_R_i64_err io_SectionReader_Seek(void* self, int64_t offset, so_int whence) {
    io_SectionReader* s = (io_SectionReader*)self;
    if (whence == io_SeekStart) {
        offset += s->base;
    } else if (whence == io_SeekCurrent) {
        offset += s->off;
    } else if (whence == io_SeekEnd) {
        offset += s->limit;
    } else {
        return (so_R_i64_err){.val = 0, .err = io_ErrWhence};
    }
    if (offset < s->base) {
        return (so_R_i64_err){.val = 0, .err = io_ErrOffset};
    }
    s->off = offset;
    return (so_R_i64_err){.val = offset - s->base, .err = NULL};
}

so_R_int_err io_SectionReader_ReadAt(void* self, so_Slice p, int64_t off) {
    io_SectionReader* s = (io_SectionReader*)self;
    if (off < 0 || off >= io_SectionReader_Size(s)) {
        return (so_R_int_err){.val = 0, .err = io_EOF};
    }
    off += s->base;
    {
        int64_t max = s->limit - off;
        if ((int64_t)(so_len(p)) > max) {
            p = so_slice(so_byte, p, 0, max);
            so_R_int_err _res1 = s->r.ReadAt(s->r.self, p, off);
            so_int n = _res1.val;
            so_Error err = _res1.err;
            if (err == NULL) {
                err = io_EOF;
            }
            return (so_R_int_err){.val = n, .err = err};
        }
    }
    return s->r.ReadAt(s->r.self, p, off);
}

// Size returns the size of the section in bytes.
int64_t io_SectionReader_Size(void* self) {
    io_SectionReader* s = (io_SectionReader*)self;
    return s->limit - s->base;
}

// Outer returns the underlying [ReaderAt] and offsets for the section.
//
// The returned values are the same that were passed to [NewSectionReader]
// when the [SectionReader] was created.
io_ReaderAtOffset io_SectionReader_Outer(void* self) {
    io_SectionReader* s = (io_SectionReader*)self;
    return (io_ReaderAtOffset){s->r, s->base, s->n};
}

// -- io.go --

// Copy copies from src to dst until either EOF is reached
// on src or an error occurs. It returns the number of bytes
// copied and the first error encountered while copying, if any.
//
// A successful Copy returns err == nil, not err == EOF.
// Because Copy is defined to read from src until EOF, it does
// not treat an EOF from Read as an error to be reported.
//
// Copy allocates a buffer on the stack to hold data during the copy.
so_R_i64_err io_Copy(io_Writer dst, io_Reader src) {
    // 8 KiB
    so_int size = 8 * 1024;
    bool ok = (src.Read == io_LimitedReader_Read);
    if (ok) {
        io_LimitedReader* l = (io_LimitedReader*)src.self;
        if ((int64_t)(size) > l->N) {
            if (l->N < 1) {
                size = 1;
            } else {
                size = (so_int)(l->N);
            }
        }
    }
    so_Slice buf = so_make_slice(so_byte, size, size);
    int64_t written = 0;
    so_Error err = NULL;
    for (;;) {
        so_R_int_err _res1 = src.Read(src.self, buf);
        so_int nr = _res1.val;
        so_Error er = _res1.err;
        if (nr > 0) {
            so_R_int_err _res2 = dst.Write(dst.self, so_slice(so_byte, buf, 0, nr));
            so_int nw = _res2.val;
            so_Error ew = _res2.err;
            if (nw < 0 || nr < nw) {
                nw = 0;
                if (ew == NULL) {
                    ew = io_ErrInvalidWrite;
                }
            }
            written += (int64_t)(nw);
            if (ew != NULL) {
                err = ew;
                break;
            }
            if (nr != nw) {
                err = io_ErrShortWrite;
                break;
            }
        }
        if (er != NULL) {
            if (er != io_EOF) {
                err = er;
            }
            break;
        }
    }
    return (so_R_i64_err){.val = written, .err = err};
}

// CopyN copies n bytes (or until an error) from src to dst.
// It returns the number of bytes copied and the earliest
// error encountered while copying.
// On return, written == n if and only if err == nil.
//
// Allocates a buffer on the stack to hold data during the copy.
so_R_i64_err io_CopyN(io_Writer dst, io_Reader src, int64_t n) {
    io_LimitedReader* r = &(io_LimitedReader){src, n};
    so_R_i64_err _res1 = io_Copy(dst, (io_Reader){.self = r, .Read = io_LimitedReader_Read});
    int64_t written = _res1.val;
    so_Error err = _res1.err;
    if (written == n) {
        return (so_R_i64_err){.val = n, .err = NULL};
    }
    if (written < n && err == NULL) {
        // src stopped early; must have been EOF.
        err = io_EOF;
    }
    return (so_R_i64_err){.val = written, .err = err};
}

// ReadAll reads from r until an error or EOF and returns the data it read.
// A successful call returns err == nil, not err == EOF. Because ReadAll is
// defined to read from src until EOF, it does not treat an EOF from Read
// as an error to be reported.
//
// If the allocator is nil, uses the system allocator.
// The returned slice is allocated; the caller owns it.
so_R_slice_err io_ReadAll(mem_Allocator a, io_Reader r) {
    // Build slices of exponentially growing size,
    // then copy into a perfectly-sized slice at the end.
    so_Slice b = mem_AllocSlice(so_byte, (a), (0), (512));
    // Starting with next equal to 256 (instead of say 512 or 1024)
    // allows less memory usage for small inputs that finish in the
    // early growth stages, but we grow the read sizes quickly such that
    // it does not materially impact medium or large inputs.
    so_int next = 256;
    so_Slice chunks = so_make_slice(so_Slice, 0, 4);
    // Invariant: finalSize = sum(len(c) for c in chunks)
    so_int finalSize = 0;
    for (;;) {
        so_R_int_err _res1 = r.Read(r.self, so_slice(so_byte, b, so_len(b), so_cap(b)));
        so_int n = _res1.val;
        so_Error err = _res1.err;
        b = so_slice(so_byte, b, 0, so_len(b) + n);
        if (err != NULL) {
            if (err == io_EOF) {
                err = NULL;
            }
            if (so_len(chunks) == 0) {
                return (so_R_slice_err){.val = b, .err = err};
            }
            // Build our final right-sized slice.
            finalSize += so_len(b);
            so_Slice final = mem_AllocSlice(so_byte, (a), (0), (finalSize));
            for (so_int _ = 0; _ < so_len(chunks); _++) {
                so_Slice chunk = so_at(so_Slice, chunks, _);
                final = so_extend(so_byte, final, (chunk));
            }
            final = so_extend(so_byte, final, (b));
            // Free the intermediate slices.
            for (so_int _ = 0; _ < so_len(chunks); _++) {
                so_Slice chunk = so_at(so_Slice, chunks, _);
                mem_FreeSlice(so_byte, (a), (chunk));
            }
            mem_FreeSlice(so_byte, (a), (b));
            return (so_R_slice_err){.val = final, .err = err};
        }
        if (so_cap(b) - so_len(b) < so_cap(b) / 16) {
            // Move to the next intermediate slice.
            chunks = so_append(so_Slice, chunks, b);
            finalSize += so_len(b);
            b = mem_AllocSlice(so_byte, (a), (0), (next));
            next += next / 2;
        }
    }
}

// ReadFull reads exactly len(buf) bytes from r into buf.
// It returns the number of bytes copied and an error if fewer bytes were read.
// The error is EOF only if no bytes were read.
// If an EOF happens after reading some but not all the bytes,
// ReadFull returns [ErrUnexpectedEOF].
// On return, n == len(buf) if and only if err == nil.
// If r returns an error having read at least len(buf) bytes, the error is dropped.
so_R_int_err io_ReadFull(io_Reader r, so_Slice buf) {
    so_int n = 0;
    so_Error err = NULL;
    for (; n < so_len(buf) && err == NULL;) {
        so_int nn = 0;
        so_R_int_err _res1 = r.Read(r.self, so_slice(so_byte, buf, n, buf.len));
        nn = _res1.val;
        err = _res1.err;
        n += nn;
    }
    if (n >= so_len(buf)) {
        err = NULL;
    } else if (n > 0 && err == io_EOF) {
        err = io_ErrUnexpectedEOF;
    }
    return (so_R_int_err){.val = n, .err = err};
}

// WriteString writes the contents of the string s to w, which accepts a slice of bytes.
// [Writer.Write] is called exactly once.
so_R_int_err io_WriteString(io_Writer w, so_String s) {
    return w.Write(w.self, so_string_bytes(s));
}

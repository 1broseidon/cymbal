#include "tree_sitter/parser.h"
#include <assert.h>

#define MAX(a, b) ((a) > (b) ? (a) : (b))

#define VEC_RESIZE(vec, _cap)                                                  \
    void *tmp = realloc((vec).data, (_cap) * sizeof((vec).data[0]));           \
    assert(tmp != NULL);                                                       \
    (vec).data = tmp;                                                          \
    (vec).cap = (_cap);

#define VEC_GROW(vec, _cap)                                                    \
    if ((vec).cap < (_cap)) {                                                  \
        VEC_RESIZE((vec), (_cap));                                             \
    }

#define VEC_PUSH(vec, el)                                                      \
    if ((vec).cap == (vec).len) {                                              \
        VEC_RESIZE((vec), MAX(16, (vec).len * 2));                             \
    }                                                                          \
    (vec).data[(vec).len++] = (el);

#define VEC_POP(vec) (vec).len--;

#define VEC_NEW                                                                \
    { .len = 0, .cap = 0, .data = NULL }

#define VEC_BACK(vec) ((vec).data[(vec).len - 1])

#define VEC_FREE(vec)                                                          \
    {                                                                          \
        if ((vec).data != NULL)                                                \
            free((vec).data);                                                  \
    }

#define VEC_CLEAR(vec) (vec).len = 0;

enum TokenType { NEWLINE, INDENT, DEDENT };

typedef struct {
    uint32_t len;
    uint32_t cap;
    uint16_t *data;
} indent_vec;

static indent_vec indent_vec_new() {
    indent_vec vec = VEC_NEW;
    vec.data = calloc(1, sizeof(uint16_t));
    vec.cap = 1;
    return vec;
}

typedef struct {
    indent_vec indents;
} Scanner;

static inline void advance(TSLexer *lexer) { lexer->advance(lexer, false); }

static inline void skip(TSLexer *lexer) { lexer->advance(lexer, true); }

unsigned tree_sitter_nginx_external_scanner_serialize(void *payload,
                                                    char *buffer) {
    Scanner *scanner = (Scanner *)payload;
    size_t size = 0;

    int iter = 1;
    for (; iter < scanner->indents.len &&
           size < TREE_SITTER_SERIALIZATION_BUFFER_SIZE;
         ++iter) {
        buffer[size++] = (char)scanner->indents.data[iter];
    }

    return size;
}

void tree_sitter_nginx_external_scanner_deserialize(void *payload,
                                                  const char *buffer,
                                                  unsigned length) {
    Scanner *scanner = (Scanner *)payload;
    VEC_CLEAR(scanner->indents);
    VEC_PUSH(scanner->indents, 0);

    if (length > 0) {
        for (size_t i = 0; i < length; i++) {
            VEC_PUSH(scanner->indents, (unsigned char)buffer[i]);
        }
        return;
    }
}

void *tree_sitter_nginx_external_scanner_create() {
    Scanner *scanner = calloc(1, sizeof(Scanner));
    scanner->indents = indent_vec_new();
    tree_sitter_nginx_external_scanner_deserialize(scanner, NULL, 0);
    return scanner;
}

bool tree_sitter_nginx_external_scanner_scan(void *payload, TSLexer *lexer,
                                           const bool *valid_symbols) {
    Scanner *scanner = (Scanner *)payload;
    if (lexer->lookahead == '\n') {
        if (valid_symbols[NEWLINE]) {
            skip(lexer);
            lexer->result_symbol = NEWLINE;
            return true;
        }
        return false;
    }

    if (lexer->lookahead && lexer->get_column(lexer) == 0) {
        uint32_t indent_length = 0;

        // Indent tokens are zero width
        lexer->mark_end(lexer);

        for (;;) {
            if (lexer->lookahead == ' ') {
                indent_length++;
                skip(lexer);
            } else if (lexer->lookahead == '\t') {
                indent_length += 8;
                skip(lexer);
            } else {
                break;
            }
        }

        if (indent_length > VEC_BACK(scanner->indents) &&
            valid_symbols[INDENT]) {
            VEC_PUSH(scanner->indents, indent_length);
            lexer->result_symbol = INDENT;
            return true;
        }
        if (indent_length < VEC_BACK(scanner->indents) &&
            valid_symbols[DEDENT]) {
            VEC_POP(scanner->indents);
            lexer->result_symbol = DEDENT;
            return true;
        }
    }

    return false;
}

void tree_sitter_nginx_external_scanner_destroy(void *payload) {
    Scanner *scanner = (Scanner *)payload;
    VEC_FREE(scanner->indents);
    free(scanner);
}



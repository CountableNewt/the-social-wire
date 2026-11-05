#include "ArchiveChecksums.h"
#define XXH_INLINE_ALL
#include "xxhash.h"
uint64_t ingress_xxh3(const void *data, size_t count) { return XXH3_64bits(data, count); }

#ifndef WAGO_TREE_WASI_GRP_H
#define WAGO_TREE_WASI_GRP_H

#include <sys/types.h>

struct group { char *gr_name; };
static inline struct group *getgrgid(gid_t gid) {
  (void)gid;
  return 0;
}

#endif

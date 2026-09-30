#ifndef WAGO_TREE_WASI_PWD_H
#define WAGO_TREE_WASI_PWD_H

#include <sys/types.h>

struct passwd { char *pw_name; };
static inline struct passwd *getpwuid(uid_t uid) {
  (void)uid;
  return 0;
}

#endif

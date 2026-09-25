/* WASI has no subprocess facility. SQLite's optional .system shell command
 * receives a failure result; ordinary SQL evaluation is unchanged. */
int system(const char *command) {
  (void)command;
  return -1;
}

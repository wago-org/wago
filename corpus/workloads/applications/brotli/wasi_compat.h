/* Brotli's optional file-metadata copy calls chown(), which WASI lacks.
 * The streaming corpus invocation never calls that path. */
#define chown(path, owner, group) (0)

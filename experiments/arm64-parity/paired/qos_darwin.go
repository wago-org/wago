//go:build darwin && cgo

package main

/*
#include <pthread.h>
#include <pthread/qos.h>
static int benchmark_qos(void) { return pthread_set_qos_class_self_np(QOS_CLASS_USER_INTERACTIVE, 0); }
static unsigned benchmark_qos_read(void) {
 qos_class_t q = QOS_CLASS_UNSPECIFIED; int priority = 0;
 if (pthread_get_qos_class_np(pthread_self(), &q, &priority) != 0) return 0;
 return (unsigned)q;
}
*/
import "C"

func setBenchmarkQoS() (int, uint32) { return int(C.benchmark_qos()), uint32(C.benchmark_qos_read()) }

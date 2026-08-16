/* Wago semantic-corpus porting layer and runner for CoreMark.
 *
 * Reviewed adaptation (not upstream): supplies the freestanding porting stubs
 * and an exported runner that returns CoreMark's exact semantic output — the
 * per-kernel CRCs — without timing or stdio. See core_portme.h for provenance.
 */
#include "coremark.h"

/* 6K performance-run seeds (seed1=0, seed2=0, seed3=0x66, 2000-byte block):
 * these select CoreMark's published known-CRC set (crclist=0xd4b0,
 * crcmatrix=0xbe52, crcstate=0x5e47). */
volatile ee_s32 seed1_volatile = 0x0;
volatile ee_s32 seed2_volatile = 0x0;
volatile ee_s32 seed3_volatile = 0x66;
volatile ee_s32 seed4_volatile = 1; /* default single iteration */
volatile ee_s32 seed5_volatile = 0; /* -> ALL_ALGORITHMS_MASK */

ee_u32 default_num_contexts = 1;

void
portable_init(core_portable *p, int *argc, char *argv[])
{
    (void)argc;
    (void)argv;
    p->portable_id = 1;
}

void
portable_fini(core_portable *p)
{
    p->portable_id = 0;
}

void
start_time(void)
{
}

void
stop_time(void)
{
}

CORE_TICKS
get_time(void)
{
    return 0;
}

secs_ret
time_in_secs(CORE_TICKS ticks)
{
    return (secs_ret)ticks;
}

int
ee_printf(const char *fmt, ...)
{
    (void)fmt;
    return 0;
}

/* coremark_run(iterations, out_ptr):
 *   Runs the three CoreMark kernels for `iterations` iterations using the
 *   6K performance-run seeds and writes crclist, crcmatrix, crcstate, and
 *   crcfinal (each an ee_u16, little-endian) to linear memory at out_ptr.
 *   Returns the number of validation errors (0 on success).
 *
 * The host compares the four values against CoreMark's published known-CRC
 * table rather than trusting the module's own self-report.
 */
ee_u32
coremark_run(ee_u32 iterations, ee_ptr_int out_ptr)
{
    ee_u32        i, num_algorithms = 0;
    ee_u16        crc;
    ee_u16       *out = (ee_u16 *)out_ptr;
    core_results  results;
    ee_u8         stack_memblock[TOTAL_DATA_SIZE];

    results.seed1      = seed1_volatile;
    results.seed2      = seed2_volatile;
    results.seed3      = seed3_volatile;
    results.iterations = iterations;
    results.execs      = ALL_ALGORITHMS_MASK;
    results.size       = TOTAL_DATA_SIZE;
    results.crc        = 0;
    results.crclist    = 0;
    results.crcmatrix  = 0;
    results.crcstate   = 0;
    results.err        = 0;

    for (i = 0; i < NUM_ALGORITHMS; i++)
        if ((1u << i) & results.execs)
            num_algorithms++;
    results.size = results.size / num_algorithms;

    results.memblock[0] = stack_memblock;
    results.memblock[1] = stack_memblock;
    results.memblock[2] = stack_memblock + results.size;
    results.memblock[3] = stack_memblock + 2 * results.size;

    if (results.execs & ID_LIST)
        results.list = core_list_init(results.size,
                                      results.memblock[1],
                                      results.seed1);
    if (results.execs & ID_MATRIX)
        core_init_matrix(results.size,
                         results.memblock[2],
                         (ee_s32)results.seed1 | (((ee_s32)results.seed2) << 16),
                         &results.mat);
    if (results.execs & ID_STATE)
        core_init_state(results.size, results.seed1, results.memblock[3]);

    for (i = 0; i < results.iterations; i++)
    {
        crc        = core_bench_list(&results, 1);
        results.crc = crcu16(crc, results.crc);
        crc        = core_bench_list(&results, -1);
        results.crc = crcu16(crc, results.crc);
        if (i == 0)
            results.crclist = results.crc;
    }

    out[0] = results.crclist;
    out[1] = results.crcmatrix;
    out[2] = results.crcstate;
    out[3] = results.crc;
    return (ee_u32)results.err;
}

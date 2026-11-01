set pagination off
set confirm off
set disable-randomization off
handle SIGUSR1 nostop noprint pass
handle SIGUSR2 nostop noprint pass
handle SIG41 nostop noprint pass
run
thread apply all bt
info registers
x/12i $pc-16

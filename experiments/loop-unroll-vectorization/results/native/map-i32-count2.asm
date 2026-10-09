
experiments/loop-unroll-vectorization/results/native/map-i32-count2.bin:     file format binary


Disassembly of section .data:

0000000000000000 <.data>:
   0:	48 89 f3             	mov    %rsi,%rbx
   3:	4c 8b bb e0 fe ff ff 	mov    -0x120(%rbx),%r15
   a:	51                   	push   %rcx
   b:	48 8b 07             	mov    (%rdi),%rax
   e:	48 8b 4f 08          	mov    0x8(%rdi),%rcx
  12:	48 8b 57 10          	mov    0x10(%rdi),%rdx
  16:	4c 8b 47 18          	mov    0x18(%rdi),%r8
  1a:	4c 8b 4f 20          	mov    0x20(%rdi),%r9
  1e:	e8 11 00 00 00       	call   0x34
  23:	5f                   	pop    %rdi
  24:	48 89 07             	mov    %rax,(%rdi)
  27:	48 89 57 08          	mov    %rdx,0x8(%rdi)
  2b:	48 89 4f 10          	mov    %rcx,0x10(%rdi)
  2f:	4c 89 47 18          	mov    %r8,0x18(%rdi)
  33:	c3                   	ret
  34:	48 81 ec 48 00 00 00 	sub    $0x48,%rsp
  3b:	41 89 c4             	mov    %eax,%r12d
  3e:	41 89 cd             	mov    %ecx,%r13d
  41:	41 89 d6             	mov    %edx,%r14d
  44:	45 89 c3             	mov    %r8d,%r11d
  47:	44 89 cd             	mov    %r9d,%ebp
  4a:	31 c0                	xor    %eax,%eax
  4c:	45 31 d2             	xor    %r10d,%r10d
  4f:	90                   	nop
  50:	41 83 fe 02          	cmp    $0x2,%r14d
  54:	0f 82 7c 00 00 00    	jb     0xd6
  5a:	0f 1f 44 00 00       	nopl   0x0(%rax,%rax,1)
  5f:	45 89 ed             	mov    %r13d,%r13d
  62:	49 8d 7d 04          	lea    0x4(%r13),%rdi
  66:	4c 39 ff             	cmp    %r15,%rdi
  69:	0f 87 eb 00 00 00    	ja     0x15a
  6f:	46 8b 14 2b          	mov    (%rbx,%r13,1),%r10d
  73:	45 0f af d3          	imul   %r11d,%r10d
  77:	41 01 ea             	add    %ebp,%r10d
  7a:	45 89 e4             	mov    %r12d,%r12d
  7d:	49 8d 7c 24 04       	lea    0x4(%r12),%rdi
  82:	4c 39 ff             	cmp    %r15,%rdi
  85:	0f 87 cf 00 00 00    	ja     0x15a
  8b:	46 89 14 23          	mov    %r10d,(%rbx,%r12,1)
  8f:	41 83 c4 04          	add    $0x4,%r12d
  93:	41 83 c5 04          	add    $0x4,%r13d
  97:	41 83 ee 01          	sub    $0x1,%r14d
  9b:	49 8d 7d 04          	lea    0x4(%r13),%rdi
  9f:	4c 39 ff             	cmp    %r15,%rdi
  a2:	0f 87 b2 00 00 00    	ja     0x15a
  a8:	46 8b 14 2b          	mov    (%rbx,%r13,1),%r10d
  ac:	45 0f af d3          	imul   %r11d,%r10d
  b0:	41 01 ea             	add    %ebp,%r10d
  b3:	49 8d 7c 24 04       	lea    0x4(%r12),%rdi
  b8:	4c 39 ff             	cmp    %r15,%rdi
  bb:	0f 87 99 00 00 00    	ja     0x15a
  c1:	46 89 14 23          	mov    %r10d,(%rbx,%r12,1)
  c5:	41 83 c4 04          	add    $0x4,%r12d
  c9:	41 83 c5 04          	add    $0x4,%r13d
  cd:	41 83 ee 01          	sub    $0x1,%r14d
  d1:	e9 7a ff ff ff       	jmp    0x50
  d6:	66 0f 1f 84 00 00 00 	nopw   0x0(%rax,%rax,1)
  dd:	00 00 
  df:	90                   	nop
  e0:	45 85 f6             	test   %r14d,%r14d
  e3:	0f 84 41 00 00 00    	je     0x12a
  e9:	0f 1f 44 00 00       	nopl   0x0(%rax,%rax,1)
  ee:	49 8d 7d 04          	lea    0x4(%r13),%rdi
  f2:	4c 39 ff             	cmp    %r15,%rdi
  f5:	0f 87 5f 00 00 00    	ja     0x15a
  fb:	46 8b 14 2b          	mov    (%rbx,%r13,1),%r10d
  ff:	45 0f af d3          	imul   %r11d,%r10d
 103:	41 01 ea             	add    %ebp,%r10d
 106:	49 8d 7c 24 04       	lea    0x4(%r12),%rdi
 10b:	4c 39 ff             	cmp    %r15,%rdi
 10e:	0f 87 46 00 00 00    	ja     0x15a
 114:	46 89 14 23          	mov    %r10d,(%rbx,%r12,1)
 118:	41 83 c4 04          	add    $0x4,%r12d
 11c:	41 83 c5 04          	add    $0x4,%r13d
 120:	41 83 ee 01          	sub    $0x1,%r14d
 124:	0f 85 c4 ff ff ff    	jne    0xee
 12a:	4c 89 64 24 18       	mov    %r12,0x18(%rsp)
 12f:	4c 89 6c 24 20       	mov    %r13,0x20(%rsp)
 134:	4c 89 74 24 28       	mov    %r14,0x28(%rsp)
 139:	4c 89 54 24 30       	mov    %r10,0x30(%rsp)
 13e:	48 8b 44 24 18       	mov    0x18(%rsp),%rax
 143:	48 8b 54 24 20       	mov    0x20(%rsp),%rdx
 148:	48 8b 4c 24 28       	mov    0x28(%rsp),%rcx
 14d:	4c 8b 44 24 30       	mov    0x30(%rsp),%r8
 152:	48 81 c4 48 00 00 00 	add    $0x48,%rsp
 159:	c3                   	ret
 15a:	b8 ff ff ff ff       	mov    $0xffffffff,%eax
 15f:	48 8b 73 98          	mov    -0x68(%rbx),%rsi
 163:	c7 46 10 01 00 00 00 	movl   $0x1,0x10(%rsi)
 16a:	89 46 14             	mov    %eax,0x14(%rsi)
 16d:	c7 06 03 00 00 00    	movl   $0x3,(%rsi)
 173:	48 8b 63 e8          	mov    -0x18(%rbx),%rsp
 177:	c3                   	ret

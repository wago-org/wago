
experiments/loop-unroll-vectorization/results/native-modern/map-i32-guard2.bin:     file format binary


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
  50:	45 85 f6             	test   %r14d,%r14d
  53:	0f 84 8b 00 00 00    	je     0xe4
  59:	0f 1f 44 00 00       	nopl   0x0(%rax,%rax,1)
  5e:	45 89 ed             	mov    %r13d,%r13d
  61:	49 8d 7d 04          	lea    0x4(%r13),%rdi
  65:	4c 39 ff             	cmp    %r15,%rdi
  68:	0f 87 a6 00 00 00    	ja     0x114
  6e:	46 8b 14 2b          	mov    (%rbx,%r13,1),%r10d
  72:	45 0f af d3          	imul   %r11d,%r10d
  76:	41 01 ea             	add    %ebp,%r10d
  79:	45 89 e4             	mov    %r12d,%r12d
  7c:	49 8d 7c 24 04       	lea    0x4(%r12),%rdi
  81:	4c 39 ff             	cmp    %r15,%rdi
  84:	0f 87 8a 00 00 00    	ja     0x114
  8a:	46 89 14 23          	mov    %r10d,(%rbx,%r12,1)
  8e:	41 83 c4 04          	add    $0x4,%r12d
  92:	41 83 c5 04          	add    $0x4,%r13d
  96:	41 83 ee 01          	sub    $0x1,%r14d
  9a:	45 85 f6             	test   %r14d,%r14d
  9d:	0f 84 41 00 00 00    	je     0xe4
  a3:	0f 1f 44 00 00       	nopl   0x0(%rax,%rax,1)
  a8:	49 8d 7d 04          	lea    0x4(%r13),%rdi
  ac:	4c 39 ff             	cmp    %r15,%rdi
  af:	0f 87 5f 00 00 00    	ja     0x114
  b5:	46 8b 14 2b          	mov    (%rbx,%r13,1),%r10d
  b9:	45 0f af d3          	imul   %r11d,%r10d
  bd:	41 01 ea             	add    %ebp,%r10d
  c0:	49 8d 7c 24 04       	lea    0x4(%r12),%rdi
  c5:	4c 39 ff             	cmp    %r15,%rdi
  c8:	0f 87 46 00 00 00    	ja     0x114
  ce:	46 89 14 23          	mov    %r10d,(%rbx,%r12,1)
  d2:	41 83 c4 04          	add    $0x4,%r12d
  d6:	41 83 c5 04          	add    $0x4,%r13d
  da:	41 83 ee 01          	sub    $0x1,%r14d
  de:	0f 85 7a ff ff ff    	jne    0x5e
  e4:	4c 89 64 24 18       	mov    %r12,0x18(%rsp)
  e9:	4c 89 6c 24 20       	mov    %r13,0x20(%rsp)
  ee:	4c 89 74 24 28       	mov    %r14,0x28(%rsp)
  f3:	4c 89 54 24 30       	mov    %r10,0x30(%rsp)
  f8:	48 8b 44 24 18       	mov    0x18(%rsp),%rax
  fd:	48 8b 54 24 20       	mov    0x20(%rsp),%rdx
 102:	48 8b 4c 24 28       	mov    0x28(%rsp),%rcx
 107:	4c 8b 44 24 30       	mov    0x30(%rsp),%r8
 10c:	48 81 c4 48 00 00 00 	add    $0x48,%rsp
 113:	c3                   	ret
 114:	b8 ff ff ff ff       	mov    $0xffffffff,%eax
 119:	48 8b 73 98          	mov    -0x68(%rbx),%rsi
 11d:	c7 46 10 01 00 00 00 	movl   $0x1,0x10(%rsi)
 124:	89 46 14             	mov    %eax,0x14(%rsi)
 127:	c7 06 03 00 00 00    	movl   $0x3,(%rsi)
 12d:	48 8b 63 e8          	mov    -0x18(%rbx),%rsp
 131:	c3                   	ret

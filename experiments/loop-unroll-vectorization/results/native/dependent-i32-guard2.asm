
experiments/loop-unroll-vectorization/results/native/dependent-i32-guard2.bin:     file format binary


Disassembly of section .data:

0000000000000000 <.data>:
   0:	48 89 f3             	mov    %rsi,%rbx
   3:	4c 8b bb e0 fe ff ff 	mov    -0x120(%rbx),%r15
   a:	51                   	push   %rcx
   b:	48 8b 07             	mov    (%rdi),%rax
   e:	48 8b 4f 08          	mov    0x8(%rdi),%rcx
  12:	48 8b 57 10          	mov    0x10(%rdi),%rdx
  16:	4c 8b 47 18          	mov    0x18(%rdi),%r8
  1a:	e8 11 00 00 00       	call   0x30
  1f:	5f                   	pop    %rdi
  20:	48 89 07             	mov    %rax,(%rdi)
  23:	48 89 57 08          	mov    %rdx,0x8(%rdi)
  27:	48 89 4f 10          	mov    %rcx,0x10(%rdi)
  2b:	4c 89 47 18          	mov    %r8,0x18(%rdi)
  2f:	c3                   	ret
  30:	48 81 ec 38 00 00 00 	sub    $0x38,%rsp
  37:	41 89 c4             	mov    %eax,%r12d
  3a:	41 89 cd             	mov    %ecx,%r13d
  3d:	41 89 d6             	mov    %edx,%r14d
  40:	45 89 c1             	mov    %r8d,%r9d
  43:	66 0f 1f 84 00 00 00 	nopw   0x0(%rax,%rax,1)
  4a:	00 00 
  4c:	0f 1f 40 00          	nopl   0x0(%rax)
  50:	45 85 f6             	test   %r14d,%r14d
  53:	0f 84 7d 00 00 00    	je     0xd6
  59:	0f 1f 44 00 00       	nopl   0x0(%rax,%rax,1)
  5e:	45 89 ed             	mov    %r13d,%r13d
  61:	49 8d 7d 04          	lea    0x4(%r13),%rdi
  65:	4c 39 ff             	cmp    %r15,%rdi
  68:	0f 87 98 00 00 00    	ja     0x106
  6e:	46 03 0c 2b          	add    (%rbx,%r13,1),%r9d
  72:	45 89 e4             	mov    %r12d,%r12d
  75:	49 8d 7c 24 04       	lea    0x4(%r12),%rdi
  7a:	4c 39 ff             	cmp    %r15,%rdi
  7d:	0f 87 83 00 00 00    	ja     0x106
  83:	46 89 0c 23          	mov    %r9d,(%rbx,%r12,1)
  87:	41 83 c4 04          	add    $0x4,%r12d
  8b:	41 83 c5 04          	add    $0x4,%r13d
  8f:	41 83 ee 01          	sub    $0x1,%r14d
  93:	45 85 f6             	test   %r14d,%r14d
  96:	0f 84 3a 00 00 00    	je     0xd6
  9c:	0f 1f 44 00 00       	nopl   0x0(%rax,%rax,1)
  a1:	49 8d 7d 04          	lea    0x4(%r13),%rdi
  a5:	4c 39 ff             	cmp    %r15,%rdi
  a8:	0f 87 58 00 00 00    	ja     0x106
  ae:	46 03 0c 2b          	add    (%rbx,%r13,1),%r9d
  b2:	49 8d 7c 24 04       	lea    0x4(%r12),%rdi
  b7:	4c 39 ff             	cmp    %r15,%rdi
  ba:	0f 87 46 00 00 00    	ja     0x106
  c0:	46 89 0c 23          	mov    %r9d,(%rbx,%r12,1)
  c4:	41 83 c4 04          	add    $0x4,%r12d
  c8:	41 83 c5 04          	add    $0x4,%r13d
  cc:	41 83 ee 01          	sub    $0x1,%r14d
  d0:	0f 85 88 ff ff ff    	jne    0x5e
  d6:	4c 89 64 24 10       	mov    %r12,0x10(%rsp)
  db:	4c 89 6c 24 18       	mov    %r13,0x18(%rsp)
  e0:	4c 89 74 24 20       	mov    %r14,0x20(%rsp)
  e5:	4c 89 4c 24 28       	mov    %r9,0x28(%rsp)
  ea:	48 8b 44 24 10       	mov    0x10(%rsp),%rax
  ef:	48 8b 54 24 18       	mov    0x18(%rsp),%rdx
  f4:	48 8b 4c 24 20       	mov    0x20(%rsp),%rcx
  f9:	4c 8b 44 24 28       	mov    0x28(%rsp),%r8
  fe:	48 81 c4 38 00 00 00 	add    $0x38,%rsp
 105:	c3                   	ret
 106:	b8 ff ff ff ff       	mov    $0xffffffff,%eax
 10b:	48 8b 73 98          	mov    -0x68(%rbx),%rsi
 10f:	c7 46 10 01 00 00 00 	movl   $0x1,0x10(%rsi)
 116:	89 46 14             	mov    %eax,0x14(%rsi)
 119:	c7 06 03 00 00 00    	movl   $0x3,(%rsi)
 11f:	48 8b 63 e8          	mov    -0x18(%rbx),%rsp
 123:	c3                   	ret

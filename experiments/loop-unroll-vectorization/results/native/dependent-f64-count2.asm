
experiments/loop-unroll-vectorization/results/native/dependent-f64-count2.bin:     file format binary


Disassembly of section .data:

0000000000000000 <.data>:
   0:	48 81 ec 58 00 00 00 	sub    $0x58,%rsp
   7:	48 89 f3             	mov    %rsi,%rbx
   a:	48 89 4c 24 08       	mov    %rcx,0x8(%rsp)
   f:	4c 8b bb e0 fe ff ff 	mov    -0x120(%rbx),%r15
  16:	44 8b 27             	mov    (%rdi),%r12d
  19:	44 8b 6f 08          	mov    0x8(%rdi),%r13d
  1d:	44 8b 77 10          	mov    0x10(%rdi),%r14d
  21:	f2 44 0f 10 67 18    	movsd  0x18(%rdi),%xmm12
  27:	66 0f 1f 84 00 00 00 	nopw   0x0(%rax,%rax,1)
  2e:	00 00 
  30:	41 83 fe 02          	cmp    $0x2,%r14d
  34:	0f 82 76 00 00 00    	jb     0xb0
  3a:	0f 1f 44 00 00       	nopl   0x0(%rax,%rax,1)
  3f:	45 89 ed             	mov    %r13d,%r13d
  42:	49 8d 7d 08          	lea    0x8(%r13),%rdi
  46:	4c 39 ff             	cmp    %r15,%rdi
  49:	0f 87 ee 00 00 00    	ja     0x13d
  4f:	f2 46 0f 58 24 2b    	addsd  (%rbx,%r13,1),%xmm12
  55:	45 89 e4             	mov    %r12d,%r12d
  58:	49 8d 7c 24 08       	lea    0x8(%r12),%rdi
  5d:	4c 39 ff             	cmp    %r15,%rdi
  60:	0f 87 d7 00 00 00    	ja     0x13d
  66:	f2 46 0f 11 24 23    	movsd  %xmm12,(%rbx,%r12,1)
  6c:	41 83 c4 08          	add    $0x8,%r12d
  70:	41 83 c5 08          	add    $0x8,%r13d
  74:	41 83 ee 01          	sub    $0x1,%r14d
  78:	49 8d 7d 08          	lea    0x8(%r13),%rdi
  7c:	4c 39 ff             	cmp    %r15,%rdi
  7f:	0f 87 b8 00 00 00    	ja     0x13d
  85:	f2 46 0f 58 24 2b    	addsd  (%rbx,%r13,1),%xmm12
  8b:	49 8d 7c 24 08       	lea    0x8(%r12),%rdi
  90:	4c 39 ff             	cmp    %r15,%rdi
  93:	0f 87 a4 00 00 00    	ja     0x13d
  99:	f2 46 0f 11 24 23    	movsd  %xmm12,(%rbx,%r12,1)
  9f:	41 83 c4 08          	add    $0x8,%r12d
  a3:	41 83 c5 08          	add    $0x8,%r13d
  a7:	41 83 ee 01          	sub    $0x1,%r14d
  ab:	e9 80 ff ff ff       	jmp    0x30
  b0:	45 85 f6             	test   %r14d,%r14d
  b3:	0f 84 3e 00 00 00    	je     0xf7
  b9:	0f 1f 44 00 00       	nopl   0x0(%rax,%rax,1)
  be:	49 8d 7d 08          	lea    0x8(%r13),%rdi
  c2:	4c 39 ff             	cmp    %r15,%rdi
  c5:	0f 87 72 00 00 00    	ja     0x13d
  cb:	f2 46 0f 58 24 2b    	addsd  (%rbx,%r13,1),%xmm12
  d1:	49 8d 7c 24 08       	lea    0x8(%r12),%rdi
  d6:	4c 39 ff             	cmp    %r15,%rdi
  d9:	0f 87 5e 00 00 00    	ja     0x13d
  df:	f2 46 0f 11 24 23    	movsd  %xmm12,(%rbx,%r12,1)
  e5:	41 83 c4 08          	add    $0x8,%r12d
  e9:	41 83 c5 08          	add    $0x8,%r13d
  ed:	41 83 ee 01          	sub    $0x1,%r14d
  f1:	0f 85 c7 ff ff ff    	jne    0xbe
  f7:	4c 89 64 24 28       	mov    %r12,0x28(%rsp)
  fc:	4c 89 6c 24 30       	mov    %r13,0x30(%rsp)
 101:	4c 89 74 24 38       	mov    %r14,0x38(%rsp)
 106:	f2 44 0f 11 64 24 40 	movsd  %xmm12,0x40(%rsp)
 10d:	48 8b 7c 24 08       	mov    0x8(%rsp),%rdi
 112:	48 8b 44 24 28       	mov    0x28(%rsp),%rax
 117:	48 89 07             	mov    %rax,(%rdi)
 11a:	48 8b 44 24 30       	mov    0x30(%rsp),%rax
 11f:	48 89 47 08          	mov    %rax,0x8(%rdi)
 123:	48 8b 44 24 38       	mov    0x38(%rsp),%rax
 128:	48 89 47 10          	mov    %rax,0x10(%rdi)
 12c:	48 8b 44 24 40       	mov    0x40(%rsp),%rax
 131:	48 89 47 18          	mov    %rax,0x18(%rdi)
 135:	48 81 c4 58 00 00 00 	add    $0x58,%rsp
 13c:	c3                   	ret
 13d:	b8 ff ff ff ff       	mov    $0xffffffff,%eax
 142:	48 8b 73 98          	mov    -0x68(%rbx),%rsi
 146:	c7 46 10 01 00 00 00 	movl   $0x1,0x10(%rsi)
 14d:	89 46 14             	mov    %eax,0x14(%rsi)
 150:	c7 06 03 00 00 00    	movl   $0x3,(%rsi)
 156:	48 8b 63 e8          	mov    -0x18(%rbx),%rsp
 15a:	c3                   	ret

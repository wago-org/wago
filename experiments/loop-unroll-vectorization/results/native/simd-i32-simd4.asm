
experiments/loop-unroll-vectorization/results/native/simd-i32-simd4.bin:     file format binary


Disassembly of section .data:

0000000000000000 <.data>:
   0:	48 81 ec 78 00 00 00 	sub    $0x78,%rsp
   7:	48 89 f3             	mov    %rsi,%rbx
   a:	48 89 4c 24 08       	mov    %rcx,0x8(%rsp)
   f:	4c 8b bb e0 fe ff ff 	mov    -0x120(%rbx),%r15
  16:	f3 44 0f 6f 6f 18    	movdqu 0x18(%rdi),%xmm13
  1c:	44 8b 27             	mov    (%rdi),%r12d
  1f:	44 8b 6f 08          	mov    0x8(%rdi),%r13d
  23:	44 8b 77 10          	mov    0x10(%rdi),%r14d
  27:	31 c0                	xor    %eax,%eax
  29:	66 45 0f 57 e4       	xorpd  %xmm12,%xmm12
  2e:	66 90                	xchg   %ax,%ax
  30:	41 83 fe 04          	cmp    $0x4,%r14d
  34:	0f 82 18 01 00 00    	jb     0x152
  3a:	0f 1f 44 00 00       	nopl   0x0(%rax,%rax,1)
  3f:	45 89 ed             	mov    %r13d,%r13d
  42:	49 8d 7d 10          	lea    0x10(%r13),%rdi
  46:	4c 39 ff             	cmp    %r15,%rdi
  49:	0f 87 b3 01 00 00    	ja     0x202
  4f:	f3 42 0f 6f 04 2b    	movdqu (%rbx,%r13,1),%xmm0
  55:	66 41 0f fe c5       	paddd  %xmm13,%xmm0
  5a:	f3 44 0f 6f e0       	movdqu %xmm0,%xmm12
  5f:	f3 41 0f 6f c4       	movdqu %xmm12,%xmm0
  64:	45 89 e4             	mov    %r12d,%r12d
  67:	49 8d 7c 24 10       	lea    0x10(%r12),%rdi
  6c:	4c 39 ff             	cmp    %r15,%rdi
  6f:	0f 87 8d 01 00 00    	ja     0x202
  75:	f3 42 0f 7f 04 23    	movdqu %xmm0,(%rbx,%r12,1)
  7b:	41 83 c4 10          	add    $0x10,%r12d
  7f:	41 83 c5 10          	add    $0x10,%r13d
  83:	41 83 ee 01          	sub    $0x1,%r14d
  87:	49 8d 7d 10          	lea    0x10(%r13),%rdi
  8b:	4c 39 ff             	cmp    %r15,%rdi
  8e:	0f 87 6e 01 00 00    	ja     0x202
  94:	f3 42 0f 6f 04 2b    	movdqu (%rbx,%r13,1),%xmm0
  9a:	66 41 0f fe c5       	paddd  %xmm13,%xmm0
  9f:	f3 44 0f 6f e0       	movdqu %xmm0,%xmm12
  a4:	f3 41 0f 6f c4       	movdqu %xmm12,%xmm0
  a9:	49 8d 7c 24 10       	lea    0x10(%r12),%rdi
  ae:	4c 39 ff             	cmp    %r15,%rdi
  b1:	0f 87 4b 01 00 00    	ja     0x202
  b7:	f3 42 0f 7f 04 23    	movdqu %xmm0,(%rbx,%r12,1)
  bd:	41 83 c4 10          	add    $0x10,%r12d
  c1:	41 83 c5 10          	add    $0x10,%r13d
  c5:	41 83 ee 01          	sub    $0x1,%r14d
  c9:	49 8d 7d 10          	lea    0x10(%r13),%rdi
  cd:	4c 39 ff             	cmp    %r15,%rdi
  d0:	0f 87 2c 01 00 00    	ja     0x202
  d6:	f3 42 0f 6f 04 2b    	movdqu (%rbx,%r13,1),%xmm0
  dc:	66 41 0f fe c5       	paddd  %xmm13,%xmm0
  e1:	f3 44 0f 6f e0       	movdqu %xmm0,%xmm12
  e6:	f3 41 0f 6f c4       	movdqu %xmm12,%xmm0
  eb:	49 8d 7c 24 10       	lea    0x10(%r12),%rdi
  f0:	4c 39 ff             	cmp    %r15,%rdi
  f3:	0f 87 09 01 00 00    	ja     0x202
  f9:	f3 42 0f 7f 04 23    	movdqu %xmm0,(%rbx,%r12,1)
  ff:	41 83 c4 10          	add    $0x10,%r12d
 103:	41 83 c5 10          	add    $0x10,%r13d
 107:	41 83 ee 01          	sub    $0x1,%r14d
 10b:	49 8d 7d 10          	lea    0x10(%r13),%rdi
 10f:	4c 39 ff             	cmp    %r15,%rdi
 112:	0f 87 ea 00 00 00    	ja     0x202
 118:	f3 42 0f 6f 04 2b    	movdqu (%rbx,%r13,1),%xmm0
 11e:	66 41 0f fe c5       	paddd  %xmm13,%xmm0
 123:	f3 44 0f 6f e0       	movdqu %xmm0,%xmm12
 128:	f3 41 0f 6f c4       	movdqu %xmm12,%xmm0
 12d:	49 8d 7c 24 10       	lea    0x10(%r12),%rdi
 132:	4c 39 ff             	cmp    %r15,%rdi
 135:	0f 87 c7 00 00 00    	ja     0x202
 13b:	f3 42 0f 7f 04 23    	movdqu %xmm0,(%rbx,%r12,1)
 141:	41 83 c4 10          	add    $0x10,%r12d
 145:	41 83 c5 10          	add    $0x10,%r13d
 149:	41 83 ee 01          	sub    $0x1,%r14d
 14d:	e9 de fe ff ff       	jmp    0x30
 152:	66 0f 1f 84 00 00 00 	nopw   0x0(%rax,%rax,1)
 159:	00 00 
 15b:	0f 1f 44 00 00       	nopl   0x0(%rax,%rax,1)
 160:	45 85 f6             	test   %r14d,%r14d
 163:	0f 84 4d 00 00 00    	je     0x1b6
 169:	0f 1f 44 00 00       	nopl   0x0(%rax,%rax,1)
 16e:	49 8d 7d 10          	lea    0x10(%r13),%rdi
 172:	4c 39 ff             	cmp    %r15,%rdi
 175:	0f 87 87 00 00 00    	ja     0x202
 17b:	f3 42 0f 6f 04 2b    	movdqu (%rbx,%r13,1),%xmm0
 181:	66 41 0f fe c5       	paddd  %xmm13,%xmm0
 186:	f3 44 0f 6f e0       	movdqu %xmm0,%xmm12
 18b:	f3 41 0f 6f c4       	movdqu %xmm12,%xmm0
 190:	49 8d 7c 24 10       	lea    0x10(%r12),%rdi
 195:	4c 39 ff             	cmp    %r15,%rdi
 198:	0f 87 64 00 00 00    	ja     0x202
 19e:	f3 42 0f 7f 04 23    	movdqu %xmm0,(%rbx,%r12,1)
 1a4:	41 83 c4 10          	add    $0x10,%r12d
 1a8:	41 83 c5 10          	add    $0x10,%r13d
 1ac:	41 83 ee 01          	sub    $0x1,%r14d
 1b0:	0f 85 b8 ff ff ff    	jne    0x16e
 1b6:	4c 89 64 24 40       	mov    %r12,0x40(%rsp)
 1bb:	4c 89 6c 24 48       	mov    %r13,0x48(%rsp)
 1c0:	4c 89 74 24 50       	mov    %r14,0x50(%rsp)
 1c5:	f3 41 0f 6f c4       	movdqu %xmm12,%xmm0
 1ca:	f3 0f 7f 44 24 58    	movdqu %xmm0,0x58(%rsp)
 1d0:	48 8b 7c 24 08       	mov    0x8(%rsp),%rdi
 1d5:	48 8b 44 24 40       	mov    0x40(%rsp),%rax
 1da:	48 89 07             	mov    %rax,(%rdi)
 1dd:	48 8b 44 24 48       	mov    0x48(%rsp),%rax
 1e2:	48 89 47 08          	mov    %rax,0x8(%rdi)
 1e6:	48 8b 44 24 50       	mov    0x50(%rsp),%rax
 1eb:	48 89 47 10          	mov    %rax,0x10(%rdi)
 1ef:	f3 0f 6f 44 24 58    	movdqu 0x58(%rsp),%xmm0
 1f5:	f3 0f 7f 47 18       	movdqu %xmm0,0x18(%rdi)
 1fa:	48 81 c4 78 00 00 00 	add    $0x78,%rsp
 201:	c3                   	ret
 202:	b8 ff ff ff ff       	mov    $0xffffffff,%eax
 207:	48 8b 73 98          	mov    -0x68(%rbx),%rsi
 20b:	c7 46 10 01 00 00 00 	movl   $0x1,0x10(%rsi)
 212:	89 46 14             	mov    %eax,0x14(%rsi)
 215:	c7 06 03 00 00 00    	movl   $0x3,(%rsi)
 21b:	48 8b 63 e8          	mov    -0x18(%rbx),%rsp
 21f:	c3                   	ret

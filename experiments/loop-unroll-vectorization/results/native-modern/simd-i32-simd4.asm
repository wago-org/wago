
experiments/loop-unroll-vectorization/results/native-modern/simd-i32-simd4.bin:     file format binary


Disassembly of section .data:

0000000000000000 <.data>:
   0:	48 81 ec 78 00 00 00 	sub    $0x78,%rsp
   7:	48 89 f3             	mov    %rsi,%rbx
   a:	48 89 4c 24 08       	mov    %rcx,0x8(%rsp)
   f:	4c 8b bb e0 fe ff ff 	mov    -0x120(%rbx),%r15
  16:	c4 61 7a 6f 6f 18    	vmovdqu 0x18(%rdi),%xmm13
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
  49:	0f 87 b6 01 00 00    	ja     0x205
  4f:	c4 a1 7a 6f 04 2b    	vmovdqu (%rbx,%r13,1),%xmm0
  55:	c4 c1 79 fe c5       	vpaddd %xmm13,%xmm0,%xmm0
  5a:	c4 61 7a 6f e0       	vmovdqu %xmm0,%xmm12
  5f:	c4 c1 7a 6f c4       	vmovdqu %xmm12,%xmm0
  64:	45 89 e4             	mov    %r12d,%r12d
  67:	49 8d 7c 24 10       	lea    0x10(%r12),%rdi
  6c:	4c 39 ff             	cmp    %r15,%rdi
  6f:	0f 87 90 01 00 00    	ja     0x205
  75:	c4 a1 7a 7f 04 23    	vmovdqu %xmm0,(%rbx,%r12,1)
  7b:	41 83 c4 10          	add    $0x10,%r12d
  7f:	41 83 c5 10          	add    $0x10,%r13d
  83:	41 83 ee 01          	sub    $0x1,%r14d
  87:	49 8d 7d 10          	lea    0x10(%r13),%rdi
  8b:	4c 39 ff             	cmp    %r15,%rdi
  8e:	0f 87 71 01 00 00    	ja     0x205
  94:	c4 a1 7a 6f 04 2b    	vmovdqu (%rbx,%r13,1),%xmm0
  9a:	c4 c1 79 fe c5       	vpaddd %xmm13,%xmm0,%xmm0
  9f:	c4 61 7a 6f e0       	vmovdqu %xmm0,%xmm12
  a4:	c4 c1 7a 6f c4       	vmovdqu %xmm12,%xmm0
  a9:	49 8d 7c 24 10       	lea    0x10(%r12),%rdi
  ae:	4c 39 ff             	cmp    %r15,%rdi
  b1:	0f 87 4e 01 00 00    	ja     0x205
  b7:	c4 a1 7a 7f 04 23    	vmovdqu %xmm0,(%rbx,%r12,1)
  bd:	41 83 c4 10          	add    $0x10,%r12d
  c1:	41 83 c5 10          	add    $0x10,%r13d
  c5:	41 83 ee 01          	sub    $0x1,%r14d
  c9:	49 8d 7d 10          	lea    0x10(%r13),%rdi
  cd:	4c 39 ff             	cmp    %r15,%rdi
  d0:	0f 87 2f 01 00 00    	ja     0x205
  d6:	c4 a1 7a 6f 04 2b    	vmovdqu (%rbx,%r13,1),%xmm0
  dc:	c4 c1 79 fe c5       	vpaddd %xmm13,%xmm0,%xmm0
  e1:	c4 61 7a 6f e0       	vmovdqu %xmm0,%xmm12
  e6:	c4 c1 7a 6f c4       	vmovdqu %xmm12,%xmm0
  eb:	49 8d 7c 24 10       	lea    0x10(%r12),%rdi
  f0:	4c 39 ff             	cmp    %r15,%rdi
  f3:	0f 87 0c 01 00 00    	ja     0x205
  f9:	c4 a1 7a 7f 04 23    	vmovdqu %xmm0,(%rbx,%r12,1)
  ff:	41 83 c4 10          	add    $0x10,%r12d
 103:	41 83 c5 10          	add    $0x10,%r13d
 107:	41 83 ee 01          	sub    $0x1,%r14d
 10b:	49 8d 7d 10          	lea    0x10(%r13),%rdi
 10f:	4c 39 ff             	cmp    %r15,%rdi
 112:	0f 87 ed 00 00 00    	ja     0x205
 118:	c4 a1 7a 6f 04 2b    	vmovdqu (%rbx,%r13,1),%xmm0
 11e:	c4 c1 79 fe c5       	vpaddd %xmm13,%xmm0,%xmm0
 123:	c4 61 7a 6f e0       	vmovdqu %xmm0,%xmm12
 128:	c4 c1 7a 6f c4       	vmovdqu %xmm12,%xmm0
 12d:	49 8d 7c 24 10       	lea    0x10(%r12),%rdi
 132:	4c 39 ff             	cmp    %r15,%rdi
 135:	0f 87 ca 00 00 00    	ja     0x205
 13b:	c4 a1 7a 7f 04 23    	vmovdqu %xmm0,(%rbx,%r12,1)
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
 175:	0f 87 8a 00 00 00    	ja     0x205
 17b:	c4 a1 7a 6f 04 2b    	vmovdqu (%rbx,%r13,1),%xmm0
 181:	c4 c1 79 fe c5       	vpaddd %xmm13,%xmm0,%xmm0
 186:	c4 61 7a 6f e0       	vmovdqu %xmm0,%xmm12
 18b:	c4 c1 7a 6f c4       	vmovdqu %xmm12,%xmm0
 190:	49 8d 7c 24 10       	lea    0x10(%r12),%rdi
 195:	4c 39 ff             	cmp    %r15,%rdi
 198:	0f 87 67 00 00 00    	ja     0x205
 19e:	c4 a1 7a 7f 04 23    	vmovdqu %xmm0,(%rbx,%r12,1)
 1a4:	41 83 c4 10          	add    $0x10,%r12d
 1a8:	41 83 c5 10          	add    $0x10,%r13d
 1ac:	41 83 ee 01          	sub    $0x1,%r14d
 1b0:	0f 85 b8 ff ff ff    	jne    0x16e
 1b6:	4c 89 64 24 40       	mov    %r12,0x40(%rsp)
 1bb:	4c 89 6c 24 48       	mov    %r13,0x48(%rsp)
 1c0:	4c 89 74 24 50       	mov    %r14,0x50(%rsp)
 1c5:	c4 c1 7a 6f c4       	vmovdqu %xmm12,%xmm0
 1ca:	c4 e1 7a 7f 44 24 58 	vmovdqu %xmm0,0x58(%rsp)
 1d1:	48 8b 7c 24 08       	mov    0x8(%rsp),%rdi
 1d6:	48 8b 44 24 40       	mov    0x40(%rsp),%rax
 1db:	48 89 07             	mov    %rax,(%rdi)
 1de:	48 8b 44 24 48       	mov    0x48(%rsp),%rax
 1e3:	48 89 47 08          	mov    %rax,0x8(%rdi)
 1e7:	48 8b 44 24 50       	mov    0x50(%rsp),%rax
 1ec:	48 89 47 10          	mov    %rax,0x10(%rdi)
 1f0:	c4 e1 7a 6f 44 24 58 	vmovdqu 0x58(%rsp),%xmm0
 1f7:	c4 e1 7a 7f 47 18    	vmovdqu %xmm0,0x18(%rdi)
 1fd:	48 81 c4 78 00 00 00 	add    $0x78,%rsp
 204:	c3                   	ret
 205:	b8 ff ff ff ff       	mov    $0xffffffff,%eax
 20a:	48 8b 73 98          	mov    -0x68(%rbx),%rsi
 20e:	c7 46 10 01 00 00 00 	movl   $0x1,0x10(%rsi)
 215:	89 46 14             	mov    %eax,0x14(%rsi)
 218:	c7 06 03 00 00 00    	movl   $0x3,(%rsi)
 21e:	48 8b 63 e8          	mov    -0x18(%rbx),%rsp
 222:	c3                   	ret

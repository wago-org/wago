
experiments/loop-unroll-vectorization/results/native/pointer-guard2.bin:     file format binary


Disassembly of section .data:

0000000000000000 <.data>:
   0:	48 89 f3             	mov    %rsi,%rbx
   3:	4c 8b bb e0 fe ff ff 	mov    -0x120(%rbx),%r15
   a:	51                   	push   %rcx
   b:	48 8b 07             	mov    (%rdi),%rax
   e:	48 8b 4f 08          	mov    0x8(%rdi),%rcx
  12:	48 8b 57 10          	mov    0x10(%rdi),%rdx
  16:	e8 0d 00 00 00       	call   0x28
  1b:	5f                   	pop    %rdi
  1c:	48 89 07             	mov    %rax,(%rdi)
  1f:	48 89 57 08          	mov    %rdx,0x8(%rdi)
  23:	48 89 4f 10          	mov    %rcx,0x10(%rdi)
  27:	c3                   	ret
  28:	48 81 ec 38 00 00 00 	sub    $0x38,%rsp
  2f:	41 89 c4             	mov    %eax,%r12d
  32:	41 89 cd             	mov    %ecx,%r13d
  35:	41 89 d6             	mov    %edx,%r14d
  38:	66 0f 1f 84 00 00 00 	nopw   0x0(%rax,%rax,1)
  3f:	00 00 
  41:	0f 1f 80 00 00 00 00 	nopl   0x0(%rax)
  48:	45 85 ed             	test   %r13d,%r13d
  4b:	0f 84 57 00 00 00    	je     0xa8
  51:	0f 1f 44 00 00       	nopl   0x0(%rax,%rax,1)
  56:	45 89 e4             	mov    %r12d,%r12d
  59:	49 8d 7c 24 04       	lea    0x4(%r12),%rdi
  5e:	4c 39 ff             	cmp    %r15,%rdi
  61:	0f 87 67 00 00 00    	ja     0xce
  67:	42 8b 3c 23          	mov    (%rbx,%r12,1),%edi
  6b:	41 89 fc             	mov    %edi,%r12d
  6e:	45 01 e6             	add    %r12d,%r14d
  71:	41 83 ed 01          	sub    $0x1,%r13d
  75:	45 85 ed             	test   %r13d,%r13d
  78:	0f 84 2a 00 00 00    	je     0xa8
  7e:	0f 1f 44 00 00       	nopl   0x0(%rax,%rax,1)
  83:	45 89 e4             	mov    %r12d,%r12d
  86:	49 8d 7c 24 04       	lea    0x4(%r12),%rdi
  8b:	4c 39 ff             	cmp    %r15,%rdi
  8e:	0f 87 3a 00 00 00    	ja     0xce
  94:	42 8b 3c 23          	mov    (%rbx,%r12,1),%edi
  98:	41 89 fc             	mov    %edi,%r12d
  9b:	45 01 e6             	add    %r12d,%r14d
  9e:	41 83 ed 01          	sub    $0x1,%r13d
  a2:	0f 85 ae ff ff ff    	jne    0x56
  a8:	4c 89 64 24 10       	mov    %r12,0x10(%rsp)
  ad:	4c 89 6c 24 18       	mov    %r13,0x18(%rsp)
  b2:	4c 89 74 24 20       	mov    %r14,0x20(%rsp)
  b7:	48 8b 44 24 10       	mov    0x10(%rsp),%rax
  bc:	48 8b 54 24 18       	mov    0x18(%rsp),%rdx
  c1:	48 8b 4c 24 20       	mov    0x20(%rsp),%rcx
  c6:	48 81 c4 38 00 00 00 	add    $0x38,%rsp
  cd:	c3                   	ret
  ce:	b8 0c 00 00 00       	mov    $0xc,%eax
  d3:	48 8b 73 98          	mov    -0x68(%rbx),%rsi
  d7:	c7 46 10 01 00 00 00 	movl   $0x1,0x10(%rsi)
  de:	89 46 14             	mov    %eax,0x14(%rsi)
  e1:	c7 06 03 00 00 00    	movl   $0x3,(%rsi)
  e7:	48 8b 63 e8          	mov    -0x18(%rbx),%rsp
  eb:	c3                   	ret

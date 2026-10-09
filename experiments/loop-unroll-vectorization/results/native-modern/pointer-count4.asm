
experiments/loop-unroll-vectorization/results/native-modern/pointer-count4.bin:     file format binary


Disassembly of section .data:

0000000000000000 <.data>:
   0:	48 89 f3             	mov    %rsi,%rbx
   3:	4c 8b bb e0 fe ff ff 	mov    -0x120(%rbx),%r15
   a:	51                   	push   %rcx
   b:	48 8b 07             	mov    (%rdi),%rax
   e:	48 8b 4f 08          	mov    0x8(%rdi),%rcx
  12:	48 8b 57 10          	mov    0x10(%rdi),%rdx
  16:	e8 15 00 00 00       	call   0x30
  1b:	5f                   	pop    %rdi
  1c:	48 89 07             	mov    %rax,(%rdi)
  1f:	48 89 57 08          	mov    %rdx,0x8(%rdi)
  23:	48 89 4f 10          	mov    %rcx,0x10(%rdi)
  27:	c3                   	ret
  28:	0f 1f 84 00 00 00 00 	nopl   0x0(%rax,%rax,1)
  2f:	00 
  30:	48 81 ec 38 00 00 00 	sub    $0x38,%rsp
  37:	41 89 c4             	mov    %eax,%r12d
  3a:	41 89 cd             	mov    %ecx,%r13d
  3d:	41 89 d6             	mov    %edx,%r14d
  40:	41 83 fd 04          	cmp    $0x4,%r13d
  44:	0f 82 80 00 00 00    	jb     0xca
  4a:	0f 1f 44 00 00       	nopl   0x0(%rax,%rax,1)
  4f:	45 89 e4             	mov    %r12d,%r12d
  52:	49 8d 7c 24 04       	lea    0x4(%r12),%rdi
  57:	4c 39 ff             	cmp    %r15,%rdi
  5a:	0f 87 c6 00 00 00    	ja     0x126
  60:	42 8b 3c 23          	mov    (%rbx,%r12,1),%edi
  64:	41 89 fc             	mov    %edi,%r12d
  67:	45 01 e6             	add    %r12d,%r14d
  6a:	41 83 ed 01          	sub    $0x1,%r13d
  6e:	45 89 e4             	mov    %r12d,%r12d
  71:	49 8d 7c 24 04       	lea    0x4(%r12),%rdi
  76:	4c 39 ff             	cmp    %r15,%rdi
  79:	0f 87 a7 00 00 00    	ja     0x126
  7f:	42 8b 3c 23          	mov    (%rbx,%r12,1),%edi
  83:	41 89 fc             	mov    %edi,%r12d
  86:	45 01 e6             	add    %r12d,%r14d
  89:	41 83 ed 01          	sub    $0x1,%r13d
  8d:	49 8d 7c 24 04       	lea    0x4(%r12),%rdi
  92:	4c 39 ff             	cmp    %r15,%rdi
  95:	0f 87 8b 00 00 00    	ja     0x126
  9b:	42 8b 3c 23          	mov    (%rbx,%r12,1),%edi
  9f:	41 89 fc             	mov    %edi,%r12d
  a2:	45 01 e6             	add    %r12d,%r14d
  a5:	41 83 ed 01          	sub    $0x1,%r13d
  a9:	49 8d 7c 24 04       	lea    0x4(%r12),%rdi
  ae:	4c 39 ff             	cmp    %r15,%rdi
  b1:	0f 87 6f 00 00 00    	ja     0x126
  b7:	42 8b 3c 23          	mov    (%rbx,%r12,1),%edi
  bb:	41 89 fc             	mov    %edi,%r12d
  be:	45 01 e6             	add    %r12d,%r14d
  c1:	41 83 ed 01          	sub    $0x1,%r13d
  c5:	e9 76 ff ff ff       	jmp    0x40
  ca:	66 0f 1f 44 00 00    	nopw   0x0(%rax,%rax,1)
  d0:	45 85 ed             	test   %r13d,%r13d
  d3:	0f 84 27 00 00 00    	je     0x100
  d9:	0f 1f 44 00 00       	nopl   0x0(%rax,%rax,1)
  de:	49 8d 7c 24 04       	lea    0x4(%r12),%rdi
  e3:	4c 39 ff             	cmp    %r15,%rdi
  e6:	0f 87 3a 00 00 00    	ja     0x126
  ec:	42 8b 3c 23          	mov    (%rbx,%r12,1),%edi
  f0:	41 89 fc             	mov    %edi,%r12d
  f3:	45 01 e6             	add    %r12d,%r14d
  f6:	41 83 ed 01          	sub    $0x1,%r13d
  fa:	0f 85 de ff ff ff    	jne    0xde
 100:	4c 89 64 24 10       	mov    %r12,0x10(%rsp)
 105:	4c 89 6c 24 18       	mov    %r13,0x18(%rsp)
 10a:	4c 89 74 24 20       	mov    %r14,0x20(%rsp)
 10f:	48 8b 44 24 10       	mov    0x10(%rsp),%rax
 114:	48 8b 54 24 18       	mov    0x18(%rsp),%rdx
 119:	48 8b 4c 24 20       	mov    0x20(%rsp),%rcx
 11e:	48 81 c4 38 00 00 00 	add    $0x38,%rsp
 125:	c3                   	ret
 126:	b8 0c 00 00 00       	mov    $0xc,%eax
 12b:	48 8b 73 98          	mov    -0x68(%rbx),%rsi
 12f:	c7 46 10 01 00 00 00 	movl   $0x1,0x10(%rsi)
 136:	89 46 14             	mov    %eax,0x14(%rsi)
 139:	c7 06 03 00 00 00    	movl   $0x3,(%rsi)
 13f:	48 8b 63 e8          	mov    -0x18(%rbx),%rsp
 143:	c3                   	ret

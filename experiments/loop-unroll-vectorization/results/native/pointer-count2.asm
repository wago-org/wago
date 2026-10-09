
experiments/loop-unroll-vectorization/results/native/pointer-count2.bin:     file format binary


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
  48:	41 83 fd 02          	cmp    $0x2,%r13d
  4c:	0f 82 48 00 00 00    	jb     0x9a
  52:	0f 1f 44 00 00       	nopl   0x0(%rax,%rax,1)
  57:	45 89 e4             	mov    %r12d,%r12d
  5a:	49 8d 7c 24 04       	lea    0x4(%r12),%rdi
  5f:	4c 39 ff             	cmp    %r15,%rdi
  62:	0f 87 96 00 00 00    	ja     0xfe
  68:	42 8b 3c 23          	mov    (%rbx,%r12,1),%edi
  6c:	41 89 fc             	mov    %edi,%r12d
  6f:	45 01 e6             	add    %r12d,%r14d
  72:	41 83 ed 01          	sub    $0x1,%r13d
  76:	45 89 e4             	mov    %r12d,%r12d
  79:	49 8d 7c 24 04       	lea    0x4(%r12),%rdi
  7e:	4c 39 ff             	cmp    %r15,%rdi
  81:	0f 87 77 00 00 00    	ja     0xfe
  87:	42 8b 3c 23          	mov    (%rbx,%r12,1),%edi
  8b:	41 89 fc             	mov    %edi,%r12d
  8e:	45 01 e6             	add    %r12d,%r14d
  91:	41 83 ed 01          	sub    $0x1,%r13d
  95:	e9 ae ff ff ff       	jmp    0x48
  9a:	66 0f 1f 84 00 00 00 	nopw   0x0(%rax,%rax,1)
  a1:	00 00 
  a3:	0f 1f 44 00 00       	nopl   0x0(%rax,%rax,1)
  a8:	45 85 ed             	test   %r13d,%r13d
  ab:	0f 84 27 00 00 00    	je     0xd8
  b1:	0f 1f 44 00 00       	nopl   0x0(%rax,%rax,1)
  b6:	49 8d 7c 24 04       	lea    0x4(%r12),%rdi
  bb:	4c 39 ff             	cmp    %r15,%rdi
  be:	0f 87 3a 00 00 00    	ja     0xfe
  c4:	42 8b 3c 23          	mov    (%rbx,%r12,1),%edi
  c8:	41 89 fc             	mov    %edi,%r12d
  cb:	45 01 e6             	add    %r12d,%r14d
  ce:	41 83 ed 01          	sub    $0x1,%r13d
  d2:	0f 85 de ff ff ff    	jne    0xb6
  d8:	4c 89 64 24 10       	mov    %r12,0x10(%rsp)
  dd:	4c 89 6c 24 18       	mov    %r13,0x18(%rsp)
  e2:	4c 89 74 24 20       	mov    %r14,0x20(%rsp)
  e7:	48 8b 44 24 10       	mov    0x10(%rsp),%rax
  ec:	48 8b 54 24 18       	mov    0x18(%rsp),%rdx
  f1:	48 8b 4c 24 20       	mov    0x20(%rsp),%rcx
  f6:	48 81 c4 38 00 00 00 	add    $0x38,%rsp
  fd:	c3                   	ret
  fe:	b8 0c 00 00 00       	mov    $0xc,%eax
 103:	48 8b 73 98          	mov    -0x68(%rbx),%rsi
 107:	c7 46 10 01 00 00 00 	movl   $0x1,0x10(%rsi)
 10e:	89 46 14             	mov    %eax,0x14(%rsi)
 111:	c7 06 03 00 00 00    	movl   $0x3,(%rsi)
 117:	48 8b 63 e8          	mov    -0x18(%rbx),%rsp
 11b:	c3                   	ret

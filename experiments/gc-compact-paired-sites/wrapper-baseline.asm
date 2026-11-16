
/workspace/shared/gc-paired-site-evidence/baseline-gc-array-copy-checksum-wrapper.bin:     file format binary


Disassembly of section .data:

0000000000000000 <.data>:
   0:	48 89 f3             	mov    %rsi,%rbx
   3:	51                   	push   %rcx
   4:	48 8b 07             	mov    (%rdi),%rax
   7:	e8 05 00 00 00       	call   0x11
   c:	59                   	pop    %rcx
   d:	48 89 01             	mov    %rax,(%rcx)
  10:	c3                   	ret
  11:	48 81 ec 28 00 00 00 	sub    $0x28,%rsp
  18:	48 8b 73 b8          	mov    -0x48(%rbx),%rsi
  1c:	48 39 f4             	cmp    %rsi,%rsp
  1f:	0f 82 67 00 00 00    	jb     0x8c
  25:	41 89 c6             	mov    %eax,%r14d
  28:	31 c0                	xor    %eax,%eax
  2a:	45 31 e4             	xor    %r12d,%r12d
  2d:	45 31 ed             	xor    %r13d,%r13d
  30:	44 89 34 24          	mov    %r14d,(%rsp)
  34:	44 89 64 24 08       	mov    %r12d,0x8(%rsp)
  39:	44 89 6c 24 10       	mov    %r13d,0x10(%rsp)
  3e:	66 90                	xchg   %ax,%ax
  40:	44 89 f7             	mov    %r14d,%edi
  43:	4c 89 6c 24 18       	mov    %r13,0x18(%rsp)
  48:	48 89 f8             	mov    %rdi,%rax
  4b:	e8 e1 e5 ff ff       	call   0xffffffffffffe631
  50:	48 89 c7             	mov    %rax,%rdi
  53:	41 89 fd             	mov    %edi,%r13d
  56:	44 03 6c 24 18       	add    0x18(%rsp),%r13d
  5b:	44 8b 64 24 08       	mov    0x8(%rsp),%r12d
  60:	41 83 c4 01          	add    $0x1,%r12d
  64:	44 89 64 24 08       	mov    %r12d,0x8(%rsp)
  69:	44 89 6c 24 10       	mov    %r13d,0x10(%rsp)
  6e:	44 8b 34 24          	mov    (%rsp),%r14d
  72:	41 83 fc 40          	cmp    $0x40,%r12d
  76:	0f 82 c4 ff ff ff    	jb     0x40
  7c:	0f 1f 44 00 00       	nopl   0x0(%rax,%rax,1)
  81:	44 89 e8             	mov    %r13d,%eax
  84:	48 81 c4 28 00 00 00 	add    $0x28,%rsp
  8b:	c3                   	ret
  8c:	b8 00 00 00 00       	mov    $0x0,%eax
  91:	e9 00 00 00 00       	jmp    0x96
  96:	48 8b 73 98          	mov    -0x68(%rbx),%rsi
  9a:	c7 46 10 04 00 00 00 	movl   $0x4,0x10(%rsi)
  a1:	89 46 14             	mov    %eax,0x14(%rsi)
  a4:	c7 06 0d 00 00 00    	movl   $0xd,(%rsi)
  aa:	48 8b 63 e8          	mov    -0x18(%rbx),%rsp
  ae:	c3                   	ret

package mate

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	e2eutil "volcano.sh/volcano/test/e2e/util"
)

const (
	// A100 GPU
	ResourceA100 v1.ResourceName = "nvidia.com/A100"
	// H100 GPU
	ResourceH100 v1.ResourceName = "nvidia.com/H100"
)

var _ = Describe("KWOK Node Resource Test", func() {
	It("should increase root queue deserved when a KWOK A100 node is added", func() {
		ctx := e2eutil.InitTestContext(e2eutil.Options{
			Queues: []string{"root"},
		})

		By("Getting root queue deserved before adding node")
		rootQueue, err := ctx.Vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), "root", metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred(), "failed to get root queue")
		before := rootQueue.Spec.Deserved.DeepCopy()

		By("Adding KWOK A100 node")
		err = e2eutil.CreateKwokA100Node(ctx, "kwok-node-a100-mate-0", 8, "8Gi", 2)
		Expect(err).NotTo(HaveOccurred(), "failed to create KWOK A100 node")
		// defer func() {
		// 	err = e2eutil.DeleteKwokNode(ctx, "kwok-node-a100-mate-0")
		// 	Expect(err).NotTo(HaveOccurred(), "failed to delete KWOK A100 node")
		// }()

		By("Waiting for scheduler to update root queue resources")
		Eventually(func() v1.ResourceList {
			queue, err := ctx.Vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), "root", metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			return queue.Spec.Deserved
		}, e2eutil.TwentySecond, e2eutil.OneSecond).ShouldNot(Equal(before), "root queue deserved should grow after node added")

		By("Verifying root queue deserved resources increased")
		queue, err := ctx.Vcclient.SchedulingV1beta1().Queues().Get(context.TODO(), "root", metav1.GetOptions{})

		fmt.Printf("Root queue deserved before: %v, after: %v\n", before, queue.Spec.Deserved)
		fmt.Fprintf(GinkgoWriter, "Root queue before: %+v\n", before)
		fmt.Fprintf(GinkgoWriter, "Root queue: %+v\n", queue)
		Expect(err).NotTo(HaveOccurred())
		Expect(queue.Spec.Deserved.Cpu().Cmp(*before.Cpu())).To(Equal(1), "CPU should increase")
		Expect(queue.Spec.Deserved.Memory().Cmp(*before.Memory())).To(Equal(1), "Memory should increase")
		// Expect(queue.Spec.Deserved["nvidia.com/A100"]).To(BeNumerically(">", before["nvidia.com/A100"]), "A100 GPU should increase")
	})
})

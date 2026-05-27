export const mockTaskComments = [
  {
    id: "c1",
    task_id: "t1",
    message: "Initial implementation completed.",
    created_at: new Date().toISOString(),
    author: {
      id: "u1",
      name: "Alice",
    },
  },
  {
    id: "c2",
    task_id: "t1",
    message: "Need API review before merge.",
    created_at: new Date().toISOString(),
    author: {
      id: "u2",
      name: "Bob",
    },
  },
  {
    id: "c3",
    task_id: "t1",
    message: "Waiting for QA verification.",
    created_at: new Date().toISOString(),
    author: {
      id: "u3",
      name: "Sarah",
    },
  },
];

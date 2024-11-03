from PIL import Image, ImageFilter, ImageOps
import os
import base64
from io import BytesIO
import tyro
from thumbhash import image_to_thumbhash, thumbhash_to_image
import base64
from io import BytesIO


def generate_responsive_images(input_image_path):
    # Define the sizes and suffixes
    sizes = [(320, "-small"), (640, "-medium"), (1024, "-large"), (1920, "-xlarge")]

    # Extract the base filename and extension
    base_name, ext = os.path.splitext(input_image_path)

    # Variable to store the Base64 encoded placeholder for the largest image
    base64_placeholder = None

    width = 200
    new_height = 200

    # Process each size
    for width, suffix in sizes:
        # Open the original image
        with Image.open(input_image_path) as img:
            img = ImageOps.exif_transpose(img) or img
            # Calculate the new height maintaining the aspect ratio
            aspect_ratio = img.height / img.width
            new_height = int(width * aspect_ratio)

            # Resize the image
            resized_img = img.resize((width, new_height), Image.Resampling.LANCZOS)

            # Save the resized image
            resized_image_path = f"{base_name}{suffix}{ext}"
            resized_img.save(resized_image_path)
            print(f"Saved resized image: {resized_image_path}")

    # thumbhash
    thumbhash = image_to_thumbhash(input_image_path)
    thumbhash_image = thumbhash_to_image(thumbhash)
    thumbhash_image.save(f"{base_name}-thumbhash.png")

    # Convert the thumbhash image to Base64
    buffered = BytesIO()
    thumbhash_image.save(buffered, format="PNG")
    base64_placeholder = (
        f"data:image/png;base64,{base64.b64encode(buffered.getvalue()).decode('utf-8')}"
    )

    # Generate the Jekyll template insertion code
    base_image_name = os.path.basename(base_name)
    template_code = f"""
{{% include responsive_image.html base_image_name="{base_image_name}" alt="Your Alt Text Here" 
    placeholder='{base64_placeholder}' width="{width}" height="{new_height}" %}}
"""
    print("\nJekyll template insertion code:\n")
    print(template_code)


def main(in_path: str):
    if os.path.isfile(in_path):
        generate_responsive_images(in_path)
    elif os.path.isdir(in_path):
        for file in os.listdir(in_path):
            generate_responsive_images(os.path.join(in_path, file))


if __name__ == "__main__":
    tyro.cli(main)
